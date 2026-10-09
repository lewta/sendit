package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
)

func TestHTTPVersionGeneratedAndDryRun(t *testing.T) {
	for _, v := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			target := defaultTarget("https://example.com", "http", 1)
			target.HTTP.HTTPVersion = v
			var b bytes.Buffer
			formatConfig(&b, []config.TargetConfig{target})
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Targets[0].HTTP.HTTPVersion != v {
				t.Fatal("generator lost policy")
			}
			text := captureStdout(t, func() {
				if err := printDryRun(path, cfg, 0); err != nil {
					t.Fatal(err)
				}
			})
			label := map[int]string{0: "auto", 1: "HTTP/1.1", 2: "HTTP/2 (HTTPS)"}[v]
			if !strings.Contains(text, "HTTP POLICY") || !strings.Contains(text, label) {
				t.Fatalf("dry-run missing %s: %s", label, text)
			}
		})
	}
}
