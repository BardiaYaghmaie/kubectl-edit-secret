package main

import (
	"fmt"
	"os"

	"github.com/BardiaYaghmaie/kubectl-edit-secret/pkg/cmd"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	pflag.CommandLine = pflag.NewFlagSet("kubectl-edit-secret", pflag.ExitOnError)

	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	rootCmd := cmd.NewEditSecretCmd(streams, fmt.Sprintf("%s (commit %s, built %s)", version, commit, buildDate))
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
