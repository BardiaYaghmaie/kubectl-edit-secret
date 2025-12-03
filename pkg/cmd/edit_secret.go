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

	// Get namespace
	var err error
	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}

	// Create Kubernetes client
	restConfig, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("failed to create REST config: %w", err)
	}

	o.clientset, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Determine editor
	if o.editor == "" {
		o.editor = os.Getenv("KUBE_EDITOR")
	}
	if o.editor == "" {
		o.editor = os.Getenv("EDITOR")
	}
	if o.editor == "" {
		// Try to find an available editor
		editors := []string{"vim", "vi", "nano", "notepad"}
		for _, e := range editors {
			if _, err := exec.LookPath(e); err == nil {
				o.editor = e
				break
			}
		}
	}
	if o.editor == "" {
		return fmt.Errorf("no editor found. Set $EDITOR or $KUBE_EDITOR environment variable, or use --editor flag")
	}

	return nil
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

	// Fetch the secret
	secret, err := o.clientset.CoreV1().Secrets(o.namespace).Get(ctx, o.secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret %s: %w", o.secretName, err)
	}

	// Prepare decoded data for editing
	decodedData := make(map[string]string)
	
	if o.key != "" {
		// Edit only the specified key
		if data, ok := secret.Data[o.key]; ok {
			decodedData[o.key] = string(data)
		} else if strData, ok := secret.StringData[o.key]; ok {
			decodedData[o.key] = strData
		} else {
			// List available keys
			keys := make([]string, 0, len(secret.Data))
			for k := range secret.Data {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return fmt.Errorf("key %q not found in secret. Available keys: %s", o.key, strings.Join(keys, ", "))
		}
	} else {
		// Edit all keys
		for k, v := range secret.Data {
			decodedData[k] = string(v)
		}
	}

	if len(decodedData) == 0 {
		return fmt.Errorf("secret %s has no data", o.secretName)
	}

	// Create YAML content for editing
	yamlContent, err := yaml.Marshal(decodedData)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// Add helpful comment header
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

	editContent := header + string(yamlContent)

	// Create temp file for editing
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("kubectl-edit-secret-%s-*.yaml", o.secretName))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(editContent); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Get file info before editing
	beforeInfo, err := os.Stat(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to stat temp file: %w", err)
	}
	beforeContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read temp file: %w", err)
	}

	// Open editor
	editorPath, editorArgs := parseEditor(o.editor)
	editorArgs = append(editorArgs, tmpPath)
	
	editorCmd := exec.Command(editorPath, editorArgs...)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	// Check if file was modified
	afterInfo, err := os.Stat(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to stat temp file after edit: %w", err)
	}

	afterContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read temp file after edit: %w", err)
	}

	// Check if anything changed
	if beforeInfo.ModTime().Equal(afterInfo.ModTime()) && bytes.Equal(beforeContent, afterContent) {
		fmt.Fprintln(o.streams.Out, "Edit cancelled, no changes made.")
		return nil
	}

	// Parse edited content
	editedData, err := parseEditedContent(afterContent)
	if err != nil {
		return fmt.Errorf("failed to parse edited content: %w", err)
	}

	// Check if data actually changed
	changed := false
	for k, newVal := range editedData {
		if oldVal, ok := decodedData[k]; !ok || oldVal != newVal {
			changed = true
			break
		}
	}
	if len(editedData) != len(decodedData) {
		changed = true
	}

	if !changed {
		fmt.Fprintln(o.streams.Out, "No changes detected.")
		return nil
	}

	// Update secret with new values
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}

	if o.key != "" {
		// Only update the specific key
		if newVal, ok := editedData[o.key]; ok {
			secret.Data[o.key] = []byte(newVal)
		}
	} else {
		// Update all keys from edited data
		// First, remove keys that were deleted
		for k := range decodedData {
			if _, exists := editedData[k]; !exists {
				delete(secret.Data, k)
			}
		}
		// Then update/add keys
		for k, v := range editedData {
			secret.Data[k] = []byte(v)
		}
	}

	// Clear StringData as we're using Data
	secret.StringData = nil

	// Apply the updated secret
	_, err = o.clientset.CoreV1().Secrets(o.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}

	fmt.Fprintf(o.streams.Out, "secret/%s edited\n", o.secretName)
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
	// Remove comment lines
	lines := strings.Split(string(content), "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			cleanLines = append(cleanLines, line)
		}
	}
	cleanContent := strings.Join(cleanLines, "\n")

	// Parse YAML
	result := make(map[string]string)
	if err := yaml.Unmarshal([]byte(cleanContent), &result); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	return result, nil
}


