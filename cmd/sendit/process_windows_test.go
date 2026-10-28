//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/pidfile"
	"github.com/spf13/cobra"
)

func TestWindowsProcessCommandsReportUnsupportedSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	owner, err := pidfile.Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close() //nolint:errcheck

	for name, factory := range map[string]func() *cobra.Command{
		"stop":   stopCmd,
		"reload": reloadCmd,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := factory()
			cmd.SetArgs([]string{"--pid-file", path})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not supported on Windows") {
				t.Fatalf("%s error = %v, want unsupported", name, err)
			}
		})
	}
}
