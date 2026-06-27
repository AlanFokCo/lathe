package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0"

func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "lathe",
		Short:   "lathe — a coding agent CLI",
		Version: version,
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
