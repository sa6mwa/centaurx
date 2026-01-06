package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", version.Module(), version.Current())
			return err
		},
	}
}
