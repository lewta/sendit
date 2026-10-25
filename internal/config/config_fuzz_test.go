package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lewta/sendit/internal/policy"
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
	for _, shape := range []string{
		"config_trust: trusted", "policy: {mode: trusted}", "allow_env: ['*']",
		"capability_requests: {env_names: []}", "capability_requests: {overwrite: true}",
		"capability_requests: {private_networks: ['0.0.0.0/0']}",
		"capability_requests.env_names: [HOME]", "capability_requests: {env_names: [], ENV_NAMES: [HOME]}",
		"g: &g {mode: trusted}\n<<: *g", "g: &g {env_names: [HOME]}\ncapability_requests: {<<: *g}",
		"cycle: &cycle {cycle: *cycle}", "key: &key capability_requests\n*key: {env_names: [HOME]}",
		"capability_requests: {env_names: [", "---\ncapability_requests: {}\n---\nconfig_trust: trusted",
	} {
		f.Add([]byte("targets: [{url: https://example.com, type: http, weight: 1}]\n" + shape))
	}
	// Truncated YAML.
	f.Add([]byte(`pacing:`))
	// Non-YAML garbage.
	f.Add([]byte(`\x00\x01\x02`))
	// Valid request template.
	f.Add([]byte(`targets: [{url: "https://example.com/{{user}}", type: http, weight: 1, vars: {user: [alice]}}]`))
	// Unmatched template delimiter.
	f.Add([]byte(`targets: [{url: "https://example.com/{{user", type: http, weight: 1}]`))
	// Template values with Unicode and escaped JSON.
	f.Add([]byte(`targets: [{url: "https://example.com", type: http, weight: 1, vars: {name: ["世界"]}, http: {body: "{\"name\":\"{{name}}\"}"}}]`))
	for _, value := range []string{"0", "1", "2", "3", "null", `"2"`, "true", "2.0", "18446744073709551617"} {
		f.Add([]byte("h: &h {http_version: " + value + "}\ntargets: [{url: https://example.com, type: http, weight: 1, http: {<<: *h}}]"))
		f.Add([]byte("targets: {url: https://example.com, type: http, weight: 1, http: {http_version: " + value + "}}"))
		f.Add([]byte("key: &key http_version\ntargets: [{url: https://example.com, type: http, weight: 1, http: {*key: " + value + "}}]"))
		f.Add([]byte("target_defaults.http.http_version: " + value + "\ntargets: [{url: https://example.com, type: http, weight: 1}]"))
	}

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
		cfg, err := Load(path)
		if err == nil {
			for _, target := range cfg.Targets {
				if target.HTTP.HTTPVersion < 0 || target.HTTP.HTTPVersion > 2 {
					t.Fatal("accepted unsupported HTTP version")
				}
			}
		}
		access, err := policy.New(policy.ModeRestricted, policy.Grants{})
		if err != nil {
			t.Fatal(err)
		}
		restricted, err := LoadWithPolicy(path, access)
		if err == nil {
			s := restricted.Access().Summary()
			if s.Mode != policy.ModeRestricted || s.AllEnv || s.AllRead || s.AllOutput || s.AllAddresses || s.Overwrite || len(s.EnvNames) != 0 || len(s.ReadRoots) != 0 || s.OutputRoot != "" || len(s.PrivateNetworks) != 0 {
				t.Fatal("YAML promoted policy")
			}
		}
	})
}
