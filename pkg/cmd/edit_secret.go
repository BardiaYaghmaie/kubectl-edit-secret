package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

var errAborted = errors.New("aborted")

// retryableError marks errors the user can fix by editing again.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

type EditSecretOptions struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericclioptions.IOStreams

	namespace   string
	contextName string
	secretName  string
	key         string
	editor      string
	secretType  string
	typeChanged bool
	create      bool
	dryRun      bool
	yes         bool
	force       bool
	showValues  bool
	restart     bool
	sets        []string
	fromFiles   []string
	unsets      []string

	creating   bool
	warnings   []string
	pretty     map[string]bool
	client     kubernetes.Interface
	isTerminal func() bool
}

func NewEditSecretOptions(streams genericclioptions.IOStreams) *EditSecretOptions {
	return &EditSecretOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		streams:     streams,
		isTerminal: func() bool {
			f, ok := streams.In.(*os.File)
			return ok && term.IsTerminal(int(f.Fd()))
		},
	}
}

func NewEditSecretCmd(streams genericclioptions.IOStreams, version string) *cobra.Command {
	o := NewEditSecretOptions(streams)

	cmd := &cobra.Command{
		Use:     "edit-secret [SECRET_NAME] [KEY]",
		Short:   "Edit a Kubernetes secret with decoded values",
		Version: version,
		Long: `Edit a Kubernetes secret by decoding base64 values, opening in your editor,
and automatically re-encoding and applying changes.

If KEY is specified, only that key is edited. If SECRET_NAME is omitted and fzf
is installed, an interactive picker is shown.

Multi-line values are shown as YAML block scalars, JSON values are pretty-printed,
and binary values are shown as !!binary (base64) and round-trip unchanged.

Run "kubectl edit-secret completion install" to enable tab completion.`,
		Example: `  # Edit all keys in a secret
  kubectl edit-secret my-secret

  # Edit a single key
  kubectl edit-secret my-secret password

  # Pick a secret interactively (requires fzf)
  kubectl edit-secret -n production

  # Create a new TLS secret (the editor opens with tls.crt and tls.key ready to fill)
  kubectl edit-secret web-tls --create --type kubernetes.io/tls

  # Non-interactive edits; read a value from stdin to keep it out of shell history
  kubectl edit-secret my-secret --set user=admin --from-file password=- --unset old-key --yes

  # Rotate a cert and restart the workloads that use it
  kubectl edit-secret web-tls --from-file tls.crt=cert.pem --from-file tls.key=key.pem --restart

  # Preview against the API server without applying
  kubectl edit-secret my-secret --set password=x --dry-run`,
		Args:              cobra.RangeArgs(0, 2),
		ValidArgsFunction: o.completeArgs,
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			o.typeChanged = cmd.Flags().Changed("type")
			if err := o.Complete(args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}

	o.configFlags.AddFlags(cmd.Flags())
	f := cmd.Flags()
	f.StringVarP(&o.editor, "editor", "e", "", "Editor to use (defaults to $KUBE_EDITOR, $EDITOR, then vim/vi/nano)")
	f.BoolVar(&o.create, "create", false, "Create the secret if it does not exist")
	f.StringVar(&o.secretType, "type", string(corev1.SecretTypeOpaque), "Secret type when creating")
	f.BoolVar(&o.dryRun, "dry-run", false, "Show changes and validate with a server-side dry run, without applying")
	f.BoolVarP(&o.yes, "yes", "y", false, "Apply without asking for confirmation")
	f.BoolVar(&o.force, "force", false, "With --yes: apply even if the secret is managed by Helm/Argo CD/Flux/an owner or fails validation")
	f.BoolVar(&o.showValues, "show-values", false, "Show old and new values in the change summary")
	f.BoolVar(&o.restart, "restart", false, "Rollout-restart the Deployments, StatefulSets and DaemonSets that use the secret")
	f.StringArrayVar(&o.sets, "set", nil, "Set KEY=VALUE without opening an editor (repeatable)")
	f.StringArrayVar(&o.fromFiles, "from-file", nil, "Set [KEY=]PATH from a file, or KEY=- from stdin, without opening an editor (repeatable)")
	f.StringArrayVar(&o.unsets, "unset", nil, "Remove KEY without opening an editor (repeatable)")
	o.registerFlagCompletions(cmd)
	cmd.AddCommand(newCompletionCmd(streams))
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})

	return cmd
}

func (o *EditSecretOptions) Complete(args []string) error {
	if len(args) > 0 {
		o.secretName = args[0]
	}
	if len(args) > 1 {
		o.key = args[1]
	}

	client, ns, err := o.clientAndNamespace()
	if err != nil {
		return err
	}
	o.client, o.namespace = client, ns

	if raw, err := o.configFlags.ToRawKubeConfigLoader().RawConfig(); err == nil {
		o.contextName = raw.CurrentContext
	}
	if c := o.configFlags.Context; c != nil && *c != "" {
		o.contextName = *c
	}

	if o.secretName == "" {
		if o.secretName, err = o.pickSecret(context.Background()); err != nil {
			return err
		}
	}

	if o.nonInteractive() {
		return nil
	}
	return o.resolveEditor()
}

func (o *EditSecretOptions) clientAndNamespace() (kubernetes.Interface, string, error) {
	ns, _, err := o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get namespace: %w", err)
	}
	restConfig, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create REST config: %w", err)
	}
	restConfig.WarningHandler = rest.NewWarningWriter(o.streams.ErrOut, rest.WarningWriterOptions{Deduplicate: true})
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	return client, ns, nil
}

func (o *EditSecretOptions) resolveEditor() error {
	if o.editor != "" {
		return nil
	}
	for _, env := range []string{"KUBE_EDITOR", "EDITOR"} {
		if e := os.Getenv(env); e != "" {
			o.editor = e
			return nil
		}
	}
	for _, e := range []string{"vim", "vi", "nano", "notepad"} {
		if _, err := exec.LookPath(e); err == nil {
			o.editor = e
			return nil
		}
	}
	return fmt.Errorf("no editor found. Set $EDITOR or $KUBE_EDITOR environment variable, or use --editor flag")
}

func (o *EditSecretOptions) nonInteractive() bool {
	return len(o.sets)+len(o.fromFiles)+len(o.unsets) > 0
}

func (o *EditSecretOptions) Validate() error {
	if o.secretName == "" {
		return fmt.Errorf("secret name is required")
	}
	if o.key != "" && o.nonInteractive() {
		return fmt.Errorf("KEY argument cannot be combined with --set, --from-file or --unset")
	}
	if o.typeChanged && !o.create {
		return fmt.Errorf("--type only applies together with --create")
	}
	if o.create && !knownSecretType(o.secretType) && !o.force {
		return fmt.Errorf("unknown secret type %q (known: %s); use --force to create it anyway",
			o.secretType, strings.Join(knownSecretTypes, ", "))
	}
	return nil
}

func (o *EditSecretOptions) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	secret, err := o.fetch(ctx)
	if err != nil {
		return err
	}
	if secret.Immutable != nil && *secret.Immutable {
		return fmt.Errorf("secret %q is immutable; delete and recreate it to change its data", o.secretName)
	}
	if err := o.checkAccess(ctx); err != nil {
		return err
	}
	o.warnings = managedWarnings(secret)
	for _, w := range o.warnings {
		fmt.Fprintf(o.streams.ErrOut, "Warning: secret/%s is %s\n", o.secretName, w)
	}

	shown, err := o.shownData(secret)
	if err != nil {
		return err
	}

	if o.nonInteractive() {
		edited, err := applyFlagEdits(shown, o.sets, o.fromFiles, o.unsets, o.streams.In)
		if err != nil {
			return err
		}
		return o.finish(o.commit(ctx, secret, shown, edited))
	}

	return o.editLoop(ctx, secret, shown)
}

func (o *EditSecretOptions) finish(err error) error {
	if errors.Is(err, errAborted) {
		fmt.Fprintln(o.streams.Out, "Aborted, no changes made.")
		return nil
	}
	return err
}

func (o *EditSecretOptions) fetch(ctx context.Context) (*corev1.Secret, error) {
	secret, err := o.client.CoreV1().Secrets(o.namespace).Get(ctx, o.secretName, metav1.GetOptions{})
	if err == nil {
		return secret, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get secret %s: %w", o.secretName, err)
	}
	if !o.create {
		return nil, fmt.Errorf("secret %q not found in namespace %q (context %s); use --create to create it", o.secretName, o.namespace, o.contextName)
	}
	o.creating = true
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: o.secretName, Namespace: o.namespace},
		Type:       corev1.SecretType(o.secretType),
		Data:       map[string][]byte{},
	}, nil
}

func (o *EditSecretOptions) checkAccess(ctx context.Context) error {
	verb, name := "update", o.secretName
	if o.creating {
		verb, name = "create", ""
	}
	review := &authv1.SelfSubjectAccessReview{Spec: authv1.SelfSubjectAccessReviewSpec{
		ResourceAttributes: &authv1.ResourceAttributes{Namespace: o.namespace, Verb: verb, Resource: "secrets", Name: name},
	}}
	res, err := o.client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil || res.Status.Allowed {
		return nil // if we cannot check, let the API server decide
	}
	return fmt.Errorf("you are not allowed to %s secrets in namespace %q (context %s)", verb, o.namespace, o.contextName)
}

func (o *EditSecretOptions) shownData(secret *corev1.Secret) (map[string][]byte, error) {
	if o.key == "" {
		if o.creating && !o.nonInteractive() {
			return scaffold(secret.Type), nil
		}
		return copyData(secret.Data), nil
	}
	if v, ok := secret.Data[o.key]; ok {
		return map[string][]byte{o.key: v}, nil
	}
	if o.creating {
		return map[string][]byte{o.key: nil}, nil
	}
	return nil, fmt.Errorf("key %q not found in secret. Available keys: %s", o.key, strings.Join(sortedKeys(secret.Data), ", "))
}

func (o *EditSecretOptions) editLoop(ctx context.Context, secret *corev1.Secret, shown map[string][]byte) error {
	ws, err := newWorkspace()
	if err != nil {
		return err
	}
	defer ws.Close()
	var editing atomic.Bool
	defer ws.cleanupOnSignal(&editing)()

	body, pretty, err := renderData(shown)
	if err != nil {
		return err
	}
	o.pretty = pretty
	presented := o.header() + body
	path := ws.Path(o.secretName + ".yaml")
	rejected := false

	for {
		if err := os.WriteFile(path, []byte(presented), 0o600); err != nil {
			return fmt.Errorf("failed to write temp file: %w", err)
		}
		editing.Store(true)
		err := runEditor(o.editor, path)
		editing.Store(false)
		if err != nil {
			return err
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read temp file after edit: %w", err)
		}
		if string(after) == presented {
			if rejected {
				fmt.Fprintln(o.streams.Out, "Edit cancelled, your rejected edits were discarded.")
			} else {
				fmt.Fprintln(o.streams.Out, "Edit cancelled, no changes made.")
			}
			return nil
		}

		edited, err := parseContent(after)
		if err == nil {
			err = o.commit(ctx, secret, shown, normalizeJSON(edited, pretty))
		} else {
			err = &retryableError{err}
		}

		var re *retryableError
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errAborted):
			return o.finish(err)
		case errors.As(err, &re) && o.isTerminal():
			fmt.Fprintf(o.streams.ErrOut, "error: %v\nReopening editor...\n", err)
			presented = withErrorHeader(after, err)
			rejected = true
		default:
			return o.saveAndFail(after, err)
		}
	}
}

func (o *EditSecretOptions) saveAndFail(content []byte, err error) error {
	saved, saveErr := saveEdits(o.secretName, content)
	if saveErr != nil {
		return fmt.Errorf("%w (also failed to save your edits: %v)", err, saveErr)
	}
	reason := err.Error()
	if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
		reason = fmt.Sprintf("secret %q was changed by someone else while you were editing", o.secretName)
	}
	return fmt.Errorf("%s\n\nYour edits were saved to:\n  %s\n(plain text, delete it when you are done)", reason, saved)
}

func (o *EditSecretOptions) header() string {
	action := "Editing"
	if o.creating {
		action = "Creating"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s secret: %s\n# Namespace: %s\n# Context: %s\n", action, o.secretName, o.namespace, o.contextName)
	for _, w := range o.warnings {
		fmt.Fprintf(&b, "# WARNING: this secret is %s\n", w)
	}
	b.WriteString(`#
# Values are DECODED (plain text) and re-encoded on save.
# Multi-line values use YAML block scalars (|). JSON is pretty-printed and
# compacted again on save. Binary values are tagged !!binary (base64).
# Add, change or remove keys. Save and exit to apply; exit without saving to cancel.
#
`)
	return b.String()
}

func (o *EditSecretOptions) commit(ctx context.Context, secret *corev1.Secret, shown, edited map[string][]byte) error {
	if o.key != "" {
		if _, ok := edited[o.key]; !ok || len(edited) != 1 {
			return &retryableError{fmt.Errorf("only key %q can be edited here; edit the whole secret to add or remove keys", o.key)}
		}
	}

	newData := mergeData(secret.Data, shown, edited)
	changes := diffData(secret.Data, newData)
	if len(changes) == 0 && !o.creating {
		fmt.Fprintln(o.streams.Out, "No changes detected.")
		return nil
	}

	verb := "Changes to"
	if o.creating {
		verb = "Creating"
	}
	fmt.Fprintf(o.streams.Out, "%s secret/%s (namespace %s, context %s):\n", verb, o.secretName, o.namespace, o.contextName)
	printChanges(o.streams.Out, changes, secret.Data, newData, o.showValues)

	problems := validateData(secret.Type, changes, newData)
	for _, p := range problems {
		fmt.Fprintf(o.streams.ErrOut, "Warning: %s\n", p)
	}
	_, hasLastApplied := secret.Annotations[lastAppliedAnnotation]
	if hasLastApplied {
		fmt.Fprintf(o.streams.ErrOut, "Note: removing the %s annotation (it holds the previous values; a later kubectl apply of your manifest will overwrite this change)\n", lastAppliedAnnotation)
	}

	consumers := o.consumers(ctx)
	if len(consumers) > 0 {
		fmt.Fprintf(o.streams.Out, "Used by: %s\n", strings.Join(consumerNames(consumers), ", "))
	}

	if !o.dryRun {
		if !o.yes {
			if err := o.confirm(o.restart && len(consumers) > 0); err != nil {
				return err
			}
		} else if len(o.warnings)+len(problems) > 0 && !o.force {
			return fmt.Errorf("refusing to apply with --yes because of the warnings above; re-run with --force to apply anyway")
		}
	}

	updated := secret.DeepCopy()
	updated.Data = newData
	updated.StringData = nil
	delete(updated.Annotations, lastAppliedAnnotation)

	var dryRun []string
	if o.dryRun {
		dryRun = []string{metav1.DryRunAll}
	}

	secrets := o.client.CoreV1().Secrets(o.namespace)
	var err error
	action := "edited"
	if o.creating {
		action = "created"
		_, err = secrets.Create(ctx, updated, metav1.CreateOptions{DryRun: dryRun})
	} else {
		_, err = secrets.Update(ctx, updated, metav1.UpdateOptions{DryRun: dryRun})
		if apierrors.IsConflict(err) {
			// Retry when only metadata changed underneath us (e.g. a controller added a label).
			if latest, gerr := secrets.Get(ctx, o.secretName, metav1.GetOptions{}); gerr == nil && len(diffData(secret.Data, latest.Data)) == 0 {
				updated.ObjectMeta = *latest.ObjectMeta.DeepCopy()
				delete(updated.Annotations, lastAppliedAnnotation)
				_, err = secrets.Update(ctx, updated, metav1.UpdateOptions{DryRun: dryRun})
			}
		}
	}
	if err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			return &retryableError{err}
		}
		return err
	}

	if o.dryRun {
		fmt.Fprintf(o.streams.Out, "secret/%s would be %s (server dry run)\n", o.secretName, action)
		return nil
	}
	fmt.Fprintf(o.streams.Out, "secret/%s %s\n", o.secretName, action)

	switch {
	case o.restart:
		return o.restartConsumers(ctx, consumers)
	case len(consumers) > 0:
		fmt.Fprintln(o.streams.Out, "Tip: pods only see env var changes after a restart; use --restart next time to roll them automatically.")
	}
	return nil
}

func (o *EditSecretOptions) confirm(withRestart bool) error {
	if !o.isTerminal() {
		return fmt.Errorf("refusing to apply without confirmation: stdin is not a terminal (pass --yes)")
	}
	prompt := "Apply these changes"
	if withRestart {
		prompt += " and restart the workloads above"
	}
	fmt.Fprintf(o.streams.Out, "%s to context %s? [y/N]: ", prompt, o.contextName)
	line, _ := bufio.NewReader(o.streams.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return errAborted
}

func (o *EditSecretOptions) pickSecret(ctx context.Context) (string, error) {
	list, err := o.client.CoreV1().Secrets(o.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("secret name is required (could not list secrets to pick from: %w)", err)
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("no secrets in namespace %q", o.namespace)
	}

	fzf, lookErr := exec.LookPath("fzf")
	if lookErr != nil || !o.isTerminal() {
		w := tabwriter.NewWriter(o.streams.ErrOut, 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "Secrets in namespace %s:\n", o.namespace)
		for _, s := range list.Items {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", s.Name, s.Type, plural(len(s.Data), "key"))
		}
		w.Flush()
		return "", fmt.Errorf("secret name is required (install fzf for an interactive picker)")
	}

	var lines strings.Builder
	for _, s := range list.Items {
		fmt.Fprintf(&lines, "%s\t%s · %s\n", s.Name, s.Type, plural(len(s.Data), "key"))
	}
	c := exec.Command(fzf, "--height=40%", "--reverse", "--prompt=secret> ", "--delimiter=\t", "--nth=1", "--tabstop=4")
	c.Stdin = strings.NewReader(lines.String())
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("no secret selected")
	}
	name, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	return name, nil
}

// runEditor runs the editor through the shell, like kubectl edit, so quoted
// arguments and paths with spaces in $EDITOR work.
func runEditor(editor, path string) error {
	if strings.TrimSpace(editor) == "" {
		return fmt.Errorf("empty editor command")
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/C", editor+` "`+path+`"`)
	} else {
		c = exec.Command("sh", "-c", editor+` "$1"`, "editor", path)
	}
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}
	return nil
}
