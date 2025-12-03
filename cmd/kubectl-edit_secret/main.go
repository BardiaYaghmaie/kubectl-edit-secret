package main

import (
	"os"

	"github.com/BardiaYaghmaie/kubectl-edit-secret/pkg/cmd"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	flags := pflag.NewFlagSet("kubectl-edit-secret", pflag.ExitOnError)
	pflag.CommandLine = flags

	streams := genericclioptions.IOStreams{
		In:     os.Stdin,
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	rootCmd := cmd.NewEditSecretCmd(streams)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
