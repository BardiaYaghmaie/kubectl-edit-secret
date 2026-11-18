package cmd

import (
	"context"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		return []string{
			"Opaque",
			"kubernetes.io/tls",
			"kubernetes.io/dockerconfigjson",
			"kubernetes.io/basic-auth",
			"kubernetes.io/ssh-auth",
		}, noFile
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
			names = append(names, s.Name)
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
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys
}
