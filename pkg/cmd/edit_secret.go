package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

var errAborted = errors.New("aborted")

// retryableError marks errors the user can fix by editing again.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

type EditSecretOptions struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericclioptions.IOStreams

	namespace  string
	secretName string
	key        string
	editor     string
	secretType string
	create     bool
	dryRun     bool
	yes        bool
	sets       []string
	fromFiles  []string
	unsets     []string

	creating   bool
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

Run "kubectl edit-secret completion install" to enable tab completion.

Multi-line values are shown as YAML block scalars. Binary values are shown as
!!binary (base64) and round-trip unchanged unless edited.`,
		Example: `  # Edit all keys in a secret
  kubectl edit-secret my-secret

  # Edit a single key
  kubectl edit-secret my-secret password

  # Pick a secret interactively (requires fzf)
  kubectl edit-secret -n production

  # Create a new secret
  kubectl edit-secret new-secret --create

  # Non-interactive edits
  kubectl edit-secret my-secret --set password=hunter2 --from-file tls.crt=./cert.pem --unset old-key --yes

  # Preview against the API server without applying
  kubectl edit-secret my-secret --set password=x --dry-run`,
		Args:              cobra.RangeArgs(0, 2),
		ValidArgsFunction: o.completeArgs,
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
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
	f.StringArrayVar(&o.sets, "set", nil, "Set KEY=VALUE without opening an editor (repeatable)")
	f.StringArrayVar(&o.fromFiles, "from-file", nil, "Set [KEY=]PATH from a file without opening an editor (repeatable)")
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

	shown, err := o.shownData(secret)
	if err != nil {
		return err
	}

	if o.nonInteractive() {
		edited, err := applyFlagEdits(shown, o.sets, o.fromFiles, o.unsets)
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
		return nil, fmt.Errorf("secret %q not found in namespace %q (use --create to create it)", o.secretName, o.namespace)
	}
	o.creating = true
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: o.secretName, Namespace: o.namespace},
		Type:       corev1.SecretType(o.secretType),
		Data:       map[string][]byte{},
	}, nil
}

func (o *EditSecretOptions) shownData(secret *corev1.Secret) (map[string][]byte, error) {
	if o.key == "" {
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

	body, err := renderData(shown)
	if err != nil {
		return err
	}
	presented := o.header() + body
	path := ws.Path(o.secretName + ".yaml")

	for {
		if err := os.WriteFile(path, []byte(presented), 0o600); err != nil {
			return fmt.Errorf("failed to write temp file: %w", err)
		}
		if err := runEditor(o.editor, path); err != nil {
			return err
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read temp file after edit: %w", err)
		}
		if string(after) == presented {
			fmt.Fprintln(o.streams.Out, "Edit cancelled, no changes made.")
			return nil
		}

		edited, err := parseContent(after)
		if err == nil {
			err = o.commit(ctx, secret, shown, edited)
		} else {
			err = &retryableError{err}
		}

		var re *retryableError
		switch {
		case err == nil:
			return nil
		case errors.As(err, &re):
			fmt.Fprintf(o.streams.ErrOut, "error: %v\nReopening editor...\n", err)
			presented = withErrorHeader(after, err)
		case apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err):
			saved, saveErr := saveEdits(o.secretName, after)
			if saveErr != nil {
				return fmt.Errorf("%w (also failed to save your edits: %v)", err, saveErr)
			}
			return fmt.Errorf("secret %q was modified by someone else while you were editing. "+
				"Your edits (plain text!) were saved to %s; re-run to edit the latest version: %w", o.secretName, saved, err)
		default:
			return o.finish(err)
		}
	}
}

func (o *EditSecretOptions) header() string {
	action := "Editing"
	if o.creating {
		action = "Creating"
	}
	return fmt.Sprintf(`# %s secret: %s
# Namespace: %s
#
# Values are DECODED (plain text) and re-encoded on save.
# Multi-line values use YAML block scalars (|). Binary values are tagged !!binary (base64).
# Add, change or remove keys. Save and exit to apply; exit without saving to cancel.
#
`, action, o.secretName, o.namespace)
}

func (o *EditSecretOptions) commit(ctx context.Context, secret *corev1.Secret, shown, edited map[string][]byte) error {
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
	fmt.Fprintf(o.streams.Out, "%s secret/%s (namespace %s):\n", verb, o.secretName, o.namespace)
	printChanges(o.streams.Out, changes)

	if !o.dryRun && !o.yes {
		if err := o.confirm(); err != nil {
			return err
		}
	}

	updated := secret.DeepCopy()
	updated.Data = newData
	updated.StringData = nil

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
	}
	if err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			return &retryableError{err}
		}
		return err
	}

	suffix := ""
	if o.dryRun {
		suffix = " (server dry run)"
	}
	fmt.Fprintf(o.streams.Out, "secret/%s %s%s\n", o.secretName, action, suffix)
	return nil
}

func (o *EditSecretOptions) confirm() error {
	if !o.isTerminal() {
		return fmt.Errorf("refusing to apply without confirmation: stdin is not a terminal (pass --yes)")
	}
	fmt.Fprint(o.streams.Out, "Apply these changes? [y/N]: ")
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
		return "", fmt.Errorf("failed to list secrets: %w", err)
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("no secrets in namespace %q", o.namespace)
	}
	names := make([]string, len(list.Items))
	for i, s := range list.Items {
		names[i] = s.Name
	}

	fzf, lookErr := exec.LookPath("fzf")
	if lookErr != nil || !o.isTerminal() {
		fmt.Fprintf(o.streams.ErrOut, "Secrets in namespace %s:\n  %s\n", o.namespace, strings.Join(names, "\n  "))
		return "", fmt.Errorf("secret name is required (install fzf for an interactive picker)")
	}

	c := exec.Command(fzf, "--height=40%", "--reverse", "--prompt=secret> ")
	c.Stdin = strings.NewReader(strings.Join(names, "\n"))
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("no secret selected")
	}
	return strings.TrimSpace(string(out)), nil
}

func runEditor(editor, path string) error {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return fmt.Errorf("empty editor command")
	}
	c := exec.Command(parts[0], append(parts[1:], path)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}
	return nil
}
