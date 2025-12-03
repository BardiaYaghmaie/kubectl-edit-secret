package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

// EditSecretOptions contains options for the edit-secret command
type EditSecretOptions struct {
	configFlags *genericclioptions.ConfigFlags
	streams     genericclioptions.IOStreams

	namespace  string
	secretName string
	key        string
	editor     string
	clientset  *kubernetes.Clientset
}

// NewEditSecretOptions creates new EditSecretOptions with default values
func NewEditSecretOptions(streams genericclioptions.IOStreams) *EditSecretOptions {
	return &EditSecretOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		streams:     streams,
	}
}

// NewEditSecretCmd creates the edit-secret cobra command
func NewEditSecretCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewEditSecretOptions(streams)

	cmd := &cobra.Command{
		Use:   "edit-secret SECRET_NAME [KEY]",
		Short: "Edit a Kubernetes secret with decoded values",
		Long: `Edit a Kubernetes secret by decoding base64 values, opening in your editor,
and automatically re-encoding and applying changes.

If KEY is specified, only that key will be edited.
Otherwise, all keys in the secret will be available for editing.

Examples:
  # Edit all keys in a secret
  kubectl edit-secret my-secret

  # Edit a specific key in a secret  
  kubectl edit-secret my-secret password

  # Edit a secret in a specific namespace
  kubectl edit-secret my-secret -n my-namespace

  # Use a specific editor
  kubectl edit-secret my-secret --editor=nano`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(cmd, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run()
		},
	}

	o.configFlags.AddFlags(cmd.Flags())
	cmd.Flags().StringVarP(&o.editor, "editor", "e", "", "Editor to use (defaults to $EDITOR, then vim, then nano)")

	return cmd
}

// Complete fills in fields required to run
func (o *EditSecretOptions) Complete(cmd *cobra.Command, args []string) error {
	o.secretName = args[0]
	if len(args) > 1 {
		o.key = args[1]
	}

	var err error
	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}

	restConfig, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to create REST config: %w", err)
	}

	o.clientset, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return o.resolveEditor()
}

// resolveEditor determines which editor to use
func (o *EditSecretOptions) resolveEditor() error {
	if o.editor != "" {
		return nil
	}

	if editor := os.Getenv("KUBE_EDITOR"); editor != "" {
		o.editor = editor
		return nil
	}

	if editor := os.Getenv("EDITOR"); editor != "" {
		o.editor = editor
		return nil
	}

	for _, e := range []string{"vim", "vi", "nano", "notepad"} {
		if _, err := exec.LookPath(e); err == nil {
			o.editor = e
			return nil
		}
	}

	return fmt.Errorf("no editor found. Set $EDITOR or $KUBE_EDITOR environment variable, or use --editor flag")
}

// Validate ensures options are valid
func (o *EditSecretOptions) Validate() error {
	if o.secretName == "" {
		return fmt.Errorf("secret name is required")
	}
	return nil
}

// Run executes the edit-secret command
func (o *EditSecretOptions) Run() error {
	ctx := context.Background()

	secret, err := o.clientset.CoreV1().Secrets(o.namespace).Get(ctx, o.secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret %s: %w", o.secretName, err)
	}

	decodedData, err := o.extractDecodedData(secret)
	if err != nil {
		return err
	}

	editedData, err := o.editInEditor(decodedData)
	if err != nil {
		return err
	}

	if editedData == nil {
		fmt.Fprintln(o.streams.Out, "Edit cancelled, no changes made.")
		return nil
	}

	if !o.hasChanges(decodedData, editedData) {
		fmt.Fprintln(o.streams.Out, "No changes detected.")
		return nil
	}

	if err := o.applyChanges(ctx, secret, decodedData, editedData); err != nil {
		return err
	}

	fmt.Fprintf(o.streams.Out, "secret/%s edited\n", o.secretName)
	return nil
}

// extractDecodedData extracts and decodes data from the secret
func (o *EditSecretOptions) extractDecodedData(secret *corev1.Secret) (map[string]string, error) {
	decodedData := make(map[string]string)

	if o.key != "" {
		return o.extractSingleKey(secret, decodedData)
	}

	for k, v := range secret.Data {
		decodedData[k] = string(v)
	}

	if len(decodedData) == 0 {
		return nil, fmt.Errorf("secret %s has no data", o.secretName)
	}

	return decodedData, nil
}

// extractSingleKey extracts a single key from the secret
func (o *EditSecretOptions) extractSingleKey(secret *corev1.Secret, decodedData map[string]string) (map[string]string, error) {
	if data, ok := secret.Data[o.key]; ok {
		decodedData[o.key] = string(data)
		return decodedData, nil
	}

	if strData, ok := secret.StringData[o.key]; ok {
		decodedData[o.key] = strData
		return decodedData, nil
	}

	keys := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return nil, fmt.Errorf("key %q not found in secret. Available keys: %s", o.key, strings.Join(keys, ", "))
}

// editInEditor opens the editor and returns edited data, or nil if cancelled
func (o *EditSecretOptions) editInEditor(decodedData map[string]string) (map[string]string, error) {
	editContent := o.createEditContent(decodedData)

	tmpPath, err := o.writeTempFile(editContent)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpPath)

	beforeContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp file: %w", err)
	}

	if err := o.runEditor(tmpPath); err != nil {
		return nil, err
	}

	afterContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp file after edit: %w", err)
	}

	if bytes.Equal(beforeContent, afterContent) {
		return nil, nil
	}

	return parseEditedContent(afterContent)
}

// createEditContent creates the YAML content with header comments
func (o *EditSecretOptions) createEditContent(decodedData map[string]string) string {
	yamlContent, _ := yaml.Marshal(decodedData)

	header := fmt.Sprintf(`# Editing secret: %s
# Namespace: %s
# 
# Modify the values below. Lines starting with '#' are ignored.
# The values shown are DECODED (plain text).
# They will be automatically base64-encoded when saved.
#
# Save and exit to apply changes. Exit without saving to cancel.
#
`, o.secretName, o.namespace)

	return header + string(yamlContent)
}

// writeTempFile creates a temporary file with the given content
func (o *EditSecretOptions) writeTempFile(content string) (string, error) {
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("kubectl-edit-secret-%s-*.yaml", o.secretName))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	return tmpPath, nil
}

// runEditor opens the editor with the given file
func (o *EditSecretOptions) runEditor(filePath string) error {
	editorPath, editorArgs := parseEditor(o.editor)
	editorArgs = append(editorArgs, filePath)

	cmd := exec.Command(editorPath, editorArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}
	return nil
}

// hasChanges checks if the edited data differs from the original
func (o *EditSecretOptions) hasChanges(original, edited map[string]string) bool {
	if len(original) != len(edited) {
		return true
	}

	for k, newVal := range edited {
		if oldVal, ok := original[k]; !ok || oldVal != newVal {
			return true
		}
	}

	return false
}

// applyChanges updates the secret with the edited data
func (o *EditSecretOptions) applyChanges(ctx context.Context, secret *corev1.Secret, original, edited map[string]string) error {
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}

	if o.key != "" {
		if newVal, ok := edited[o.key]; ok {
			secret.Data[o.key] = []byte(newVal)
		}
	} else {
		for k := range original {
			if _, exists := edited[k]; !exists {
				delete(secret.Data, k)
			}
		}
		for k, v := range edited {
			secret.Data[k] = []byte(v)
		}
	}

	secret.StringData = nil

	_, err := o.clientset.CoreV1().Secrets(o.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}

	return nil
}

// parseEditor parses the editor command into path and arguments
func parseEditor(editor string) (string, []string) {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return editor, nil
	}
	return parts[0], parts[1:]
}

// parseEditedContent parses the YAML content, ignoring comments
func parseEditedContent(content []byte) (map[string]string, error) {
	lines := strings.Split(string(content), "\n")
	cleanLines := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			cleanLines = append(cleanLines, line)
		}
	}

	result := make(map[string]string)
	if err := yaml.Unmarshal([]byte(strings.Join(cleanLines, "\n")), &result); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	return result, nil
}
