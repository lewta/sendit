package config

import (
	"os"
	"path/filepath"
	"testing"
)

// note: t.TempDir() is intentionally avoided in the fuzz body — its cleanup
// goroutine is tied to t.Context(), which Go cancels when the -fuzztime
// deadline expires, causing a spurious "context deadline exceeded" FAIL.
// os.MkdirTemp + defer os.RemoveAll sidesteps this.

// FuzzLoad feeds arbitrary YAML bytes through the config loader.
// It catches panics and unexpected parse errors on malformed input.
func FuzzLoad(f *testing.F) {
	// Minimal valid config.
	f.Add([]byte(`
pacing:
  mode: human
targets:
  - url: http://example.com
    type: http
    weight: 1
`))
	// Rate-limited mode.
	f.Add([]byte(`
pacing:
  mode: rate_limited
  requests_per_minute: 60
targets:
  - url: http://example.com
    type: http
    weight: 1
`))
	// Empty input.
	f.Add([]byte(``))
	// Truncated YAML.
	f.Add([]byte(`pacing:`))
	// Non-YAML garbage.
	f.Add([]byte(`\x00\x01\x02`))

	f.Fuzz(func(t *testing.T, data []byte) {
		dir, err := os.MkdirTemp("", "fuzz-config-*")
		if err != nil {
			t.Skip()
		}
		defer os.RemoveAll(dir)

		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Skip()
		}
		// Must not panic; validation errors are expected and fine.
		_, _ = Load(path)
	})
}
