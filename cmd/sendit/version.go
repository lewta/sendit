package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Set by goreleaser via -ldflags at build time; fallback to "dev" for local builds.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// --- version ---

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sendit %s (commit: %s, built: %s)\n", version, commit, buildDate)
		},
	}
}
