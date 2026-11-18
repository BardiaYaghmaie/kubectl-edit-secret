package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func (o *EditSecretOptions) completeArgs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return o.secretNames(toComplete), cobra.ShellCompDirectiveNoFileComp
	case 1:
		return o.secretKeys(args[0], toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func (o *EditSecretOptions) registerFlagCompletions(cmd *cobra.Command) {
	noFile := cobra.ShellCompDirectiveNoFileComp
	_ = cmd.RegisterFlagCompletionFunc("namespace", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		client, _, err := o.clientAndNamespace()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		list, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, ns := range list.Items {
			if strings.HasPrefix(ns.Name, toComplete) {
				names = append(names, ns.Name)
			}
		}
		return names, noFile
	})
	_ = cmd.RegisterFlagCompletionFunc("context", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		cfg, err := o.configFlags.ToRawKubeConfigLoader().RawConfig()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for name := range cfg.Contexts {
			if strings.HasPrefix(name, toComplete) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		return names, noFile
	})
	_ = cmd.RegisterFlagCompletionFunc("unset", func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return nil, noFile
		}
		return o.secretKeys(args[0], toComplete), noFile
	})
	_ = cmd.RegisterFlagCompletionFunc("type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return knownSecretTypes, noFile
	})
}

func (o *EditSecretOptions) secretNames(prefix string) []string {
	client, ns, err := o.clientAndNamespace()
	if err != nil {
		return nil
	}
	list, err := client.CoreV1().Secrets(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil
	}
	var names []string
	for _, s := range list.Items {
		if strings.HasPrefix(s.Name, prefix) {
			names = append(names, fmt.Sprintf("%s\t%s · %s", s.Name, s.Type, plural(len(s.Data), "key")))
		}
	}
	return names
}

func (o *EditSecretOptions) secretKeys(secretName, prefix string) []string {
	client, ns, err := o.clientAndNamespace()
	if err != nil {
		return nil
	}
	s, err := client.CoreV1().Secrets(ns).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	var keys []string
	for _, k := range sortedKeys(s.Data) {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		v := s.Data[k]
		desc := "text"
		switch {
		case !isText(v):
			desc = "binary"
		case parseCert(v) != nil:
			desc = "certificate"
		case strings.Contains(string(v), "\n"):
			desc = "multi-line"
		}
		keys = append(keys, k+"\t"+desc)
	}
	return keys
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func newCompletionCmd(streams genericclioptions.IOStreams) *cobra.Command {
	c := &cobra.Command{
		Use:    "completion [bash|zsh|fish|powershell|install]",
		Short:  "Set up shell completion",
		Hidden: true,
		Long: `Set up shell completion.

  install     Enable completion for "kubectl edit-secret <TAB>" (kubectl >= 1.26).
              Writes a kubectl_complete-edit_secret helper next to the plugin.
  bash|zsh|fish|powershell
              Print a completion script for the standalone kubectl-edit_secret binary.`,
		Example: `  kubectl edit-secret completion install
  source <(kubectl-edit_secret completion zsh)`,
		ValidArgs: []string{"install", "bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "install" {
				return installKubectlCompletion(streams)
			}
			root := cmd.Root()
			root.Use = pluginBinary
			out := streams.Out
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(out, true)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			default:
				return root.GenPowerShellCompletionWithDesc(out)
			}
		},
	}
	return c
}

const pluginBinary = "kubectl-edit_secret"

func installKubectlCompletion(streams genericclioptions.IOStreams) error {
	plugin, err := exec.LookPath(pluginBinary)
	if err != nil {
		if plugin, err = os.Executable(); err != nil {
			return fmt.Errorf("cannot locate %s: %w", pluginBinary, err)
		}
	}
	dir := filepath.Dir(plugin)
	name, script := "kubectl_complete-edit_secret", "#!/bin/sh\nexec kubectl edit-secret __complete \"$@\"\n"
	if runtime.GOOS == "windows" {
		name, script = name+".bat", "@echo off\r\nkubectl edit-secret __complete %*\r\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	fmt.Fprintf(streams.Out, "Installed %s\nTry: kubectl edit-secret <TAB>  (needs kubectl >= 1.26 and kubectl completion enabled in your shell)\n", path)
	return nil
}
