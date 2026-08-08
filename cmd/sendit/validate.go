package main

import (
	"fmt"

	"github.com/lewta/sendit/internal/config"
	"github.com/spf13/cobra"
)

// --- validate ---

func validateCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a config file",
		Long: `Parse and validate a config file without starting the engine.

Checks all config fields, pacing modes, concurrency limits, per-target
settings, and backoff parameters.

If 'targets_file' is set in the config, that file is also read and parsed
as part of validation — a missing file, malformed line, unknown driver
type, or invalid weight is reported here before any traffic is sent.

Exits 0 and prints "config valid" on success.
Exits non-zero and prints the validation error on failure.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			fmt.Println("config valid")
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config/example.yaml", "Path to YAML config file")
	return cmd
}
