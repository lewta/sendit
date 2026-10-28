package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/pidfile"
	"github.com/lewta/sendit/internal/policy"
)

// Dropping the raw policy preflight must fail before a readable secondary file
// can influence the result. Its invalid content is an observable read marker.
func TestAuthorityYAMLRejectedBeforeSecondaryRead(t *testing.T) {
	secondary := writeTempFile(t, "targets.txt", "secondary-read-marker")
	for _, attempt := range []string{
		"config_trust: trusted",
		"policy: {mode: trusted}",
		"allow_env: [HOME]",
		"capability_requests: {mode: trusted}",
		"capability_requests: {env_names: ['*']}",
		"capability_requests: {allow_env: [HOME]}",
		"capability_requests.env_names: [HOME]",
		"capability_requests: {env_names: [], env_names: [HOME]}",
		"capability_requests: {env_names: [], ENV_NAMES: [HOME]}",
		"injected: &injected {config_trust: trusted}\n<<: *injected",
		"injected: &injected {env_names: [HOME]}\ncapability_requests: {<<: *injected}",
		"injected: &injected [HOME]\ncapability_requests: {env_names: *injected}",
		"injected: &injected {self: *injected}",
	} {
		t.Run(attempt, func(t *testing.T) {
			_, err := Load(writeTemp(t, minimalValidYAML+"\ntargets_file: "+secondary+"\n"+attempt+"\n"))
			if err == nil || !strings.Contains(err.Error(), "config policy") {
				t.Fatalf("expected policy rejection before secondary read, got %v", err)
			}
			if strings.Contains(err.Error(), "secondary-read-marker") {
				t.Fatal("secondary contents leaked")
			}
		})
	}
}

func TestRestrictedPolicyEnforcesSecondaryReadGrants(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	varsPath := filepath.Join(root, "values.txt")
	targetsPath := filepath.Join(root, "targets.txt")
	if err := os.WriteFile(varsPath, []byte("alice\nbob\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetsPath, []byte("https://example.com/{{user}} http\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	granted, err := policy.New(policy.ModeRestricted, policy.Grants{ReadRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf("targets_file: %q\ntarget_defaults: {vars_file: {user: %q}}\n", targetsPath, varsPath)
	cfg, err := LoadWithPolicy(writeTemp(t, data), granted)
	if err != nil {
		t.Fatalf("granted secondary reads: %v", err)
	}
	if got := cfg.Targets[0].Vars["user"]; !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Fatalf("loaded values = %v", got)
	}

	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	for name, yaml := range map[string]string{
		"targets_file": fmt.Sprintf("targets_file: %q\ntarget_defaults: {vars: {user: [safe]}}\n", targetsPath),
		"vars_file":    fmt.Sprintf("targets: [{url: 'https://example.com/{{user}}', type: http, weight: 1, vars_file: {user: %q}}]\n", varsPath),
		"escape":       fmt.Sprintf("targets: [{url: 'https://example.com/{{user}}', type: http, weight: 1, vars_file: {user: %q}}]\n", filepath.Join(root, "..", filepath.Base(outside), "secret")),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadWithPolicy(writeTemp(t, yaml), denied)
			if err == nil || !strings.Contains(err.Error(), "config policy") {
				t.Fatalf("expected read-policy denial, got %v", err)
			}
		})
	}
}

func TestRestrictedPolicyRejectsConfigOwnedPIDPath(t *testing.T) {
	restricted, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(t.TempDir(), "sendit.pid")
	defaultPath, err := pidfile.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	uncleanDefault := filepath.Dir(defaultPath) + string(filepath.Separator) + "alias" + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(defaultPath)
	if filepath.Clean(uncleanDefault) != defaultPath {
		t.Fatalf("test path %q does not clean to %q", uncleanDefault, defaultPath)
	}
	for name, requested := range map[string]string{"custom": custom, "unclean-default-alias": uncleanDefault} {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, fmt.Sprintf("targets: [{url: https://example.com, type: http, weight: 1}]\ndaemon: {pid_file: %q}\n", requested))
			_, loadErr := LoadWithPolicy(path, restricted)
			if loadErr == nil || !strings.Contains(loadErr.Error(), "daemon.pid_file") || !strings.Contains(loadErr.Error(), "restricted") {
				t.Fatalf("restricted PID path %q was not rejected: %v", requested, loadErr)
			}
		})
	}
	if _, statErr := os.Lstat(custom); !os.IsNotExist(statErr) {
		t.Fatalf("restricted PID path was touched during config load: %v", statErr)
	}

	path := writeTemp(t, fmt.Sprintf("targets: [{url: https://example.com, type: http, weight: 1}]\ndaemon: {pid_file: %q}\n", custom))
	trusted, err := Load(path)
	if err != nil {
		t.Fatalf("trusted compatibility: %v", err)
	}
	if trusted.Daemon.PIDFile != custom {
		t.Fatalf("trusted daemon.pid_file = %q, want %q", trusted.Daemon.PIDFile, custom)
	}
}

func TestRestrictedPolicyRejectsUnsafeVariableFiles(t *testing.T) {
	root := t.TempDir()
	access, err := policy.New(policy.ModeRestricted, policy.Grants{ReadRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	load := func(path string) error {
		_, err := LoadWithPolicy(writeTemp(t, fmt.Sprintf("targets: [{url: 'https://example.com/{{user}}', type: http, weight: 1, vars_file: {user: %q}}]\n", path)), access)
		return err
	}

	oversized := filepath.Join(root, "oversized.txt")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(9 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := load(oversized); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized variable file: %v", err)
	}

	nonRegular := filepath.Join(root, "values-dir")
	if err := os.Mkdir(nonRegular, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := load(nonRegular); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("non-regular variable file: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "secret.txt")
	secret := "do-not-read-" + filepath.Base(root)
	if err := os.WriteFile(outside, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape.txt")
	if err := os.Symlink(outside, link); err == nil {
		if err := load(link); err == nil || !strings.Contains(err.Error(), "config policy") || strings.Contains(err.Error(), secret) {
			t.Fatalf("symlink escape: %v", err)
		}
	}
}

func TestRestrictedPolicyAuthorizesEffectiveAuthWithoutResolvingValues(t *testing.T) {
	const allowed = "SENDIT_ALLOWED_TOKEN"
	t.Setenv(allowed, "")
	granted, err := policy.New(policy.ModeRestricted, policy.Grants{EnvNames: []string{allowed}})
	if err != nil {
		t.Fatal(err)
	}
	for name, yaml := range map[string]string{
		"inline":  "targets: [{url: https://example.com, type: http, weight: 1, auth: {type: bearer, token_env: " + allowed + "}}]\n",
		"literal": "targets: [{url: https://example.com, type: http, weight: 1, auth: {type: bearer, token: literal, token_env: DENIED}}]\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadWithPolicy(writeTemp(t, yaml), granted); err != nil {
				t.Fatalf("authorized/deferred auth rejected: %v", err)
			}
		})
	}

	targets := writeTempFile(t, "targets.txt", "https://example.com http\n")
	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	for name, yaml := range map[string]string{
		"inline":   "targets: [{url: https://example.com, type: http, weight: 1, auth: {type: bearer, token_env: DENIED}}]\n",
		"defaults": fmt.Sprintf("targets_file: %q\ntarget_defaults: {auth: {type: basic, username: user, password_env: DENIED}}\n", targets),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadWithPolicy(writeTemp(t, yaml), denied)
			if err == nil || !strings.Contains(err.Error(), "environment variable denied: DENIED") {
				t.Fatalf("expected env-policy denial, got %v", err)
			}
		})
	}
}

func TestRestrictedPolicyPreflightsSFTPKeyReads(t *testing.T) {
	root := t.TempDir()
	key := filepath.Join(root, "id_test")
	if err := os.WriteFile(key, []byte("private-key-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf("targets: [{url: 'sftp://example.com/upload', type: sftp, weight: 1, sftp: {username: test, private_key: %q, insecure: true}}]\n", key)
	path := writeTemp(t, data)
	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithPolicy(path, denied); err == nil || !strings.Contains(err.Error(), "read path denied") || strings.Contains(err.Error(), "private-key-marker") {
		t.Fatalf("denied SFTP key error = %v", err)
	}
	granted, err := policy.New(policy.ModeRestricted, policy.Grants{ReadRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWithPolicy(path, granted); err != nil {
		t.Fatalf("granted SFTP key: %v", err)
	}
}

func TestRestrictedPolicyPreflightsOutputIntent(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing.jsonl")
	if err := os.WriteFile(existing, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootGrant := root
	withoutOverwrite, err := policy.New(policy.ModeRestricted, policy.Grants{OutputRoot: &rootGrant})
	if err != nil {
		t.Fatal(err)
	}
	for name, yaml := range map[string]string{
		"truncate": fmt.Sprintf("output: {enabled: true, file: %q, append: false}\ntargets: [{url: https://example.com, type: http, weight: 1}]\n", existing),
		"pcap":     fmt.Sprintf("output: {pcap_file: %q}\ntargets: [{url: https://example.com, type: http, weight: 1}]\n", existing),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadWithPolicy(writeTemp(t, yaml), withoutOverwrite)
			if err == nil || !strings.Contains(err.Error(), "overwrite denied") {
				t.Fatalf("expected overwrite denial, got %v", err)
			}
			got, readErr := os.ReadFile(existing)
			if readErr != nil || string(got) != "preserve" {
				t.Fatalf("denial modified output: %q, %v", got, readErr)
			}
		})
	}
	appendYAML := fmt.Sprintf("output: {enabled: true, file: %q, append: true}\ntargets: [{url: https://example.com, type: http, weight: 1}]\n", existing)
	if _, err := LoadWithPolicy(writeTemp(t, appendYAML), withoutOverwrite); err != nil {
		t.Fatalf("append should not require truncation grant: %v", err)
	}
}

func TestLoadWithPolicyTrustedCompatibility(t *testing.T) {
	if got := (&Config{}).Access().Mode(); got != policy.ModeTrusted {
		t.Fatalf("programmatic config access mode = %q", got)
	}
	dir := t.TempDir()
	vars := filepath.Join(dir, "values.txt")
	targets := filepath.Join(dir, "targets.txt")
	for path, content := range map[string]string{vars: "alice\nbob\n", targets: "https://example.com/{{user}} http\n"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{
		minimalValidYAML,
		minimalValidYAML + fmt.Sprintf("\ntargets_file: %q\ntarget_defaults: {vars_file: {user: %q}, auth: {type: bearer, token_env: TOKEN}}\noutput: {enabled: true, file: result.jsonl}\n", targets, vars),
		"targets: [{url: https://example.com, type: http, weight: 1}]",
	} {
		path := writeTemp(t, data)
		legacy, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		explicit, err := LoadWithPolicy(path, policy.Trusted())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(legacy, explicit) || explicit.Access().Mode() != policy.ModeTrusted {
			t.Fatal("trusted wrapper changed results")
		}
	}
}

func TestConfigRequestsNarrowWithoutPromotion(t *testing.T) {
	a, err := policy.New(policy.ModeRestricted, policy.Grants{EnvNames: []string{"A", "B"}})
	if err != nil {
		t.Fatal(err)
	}
	load := func(access policy.Access, request string) (*Config, error) {
		return LoadWithPolicy(writeTemp(t, minimalValidYAML+"\n"+request), access)
	}
	initial, err := load(a, "")
	if err != nil || initial.Access().CheckEnvName("B") != nil {
		t.Fatalf("omission narrowed ceiling: %v", err)
	}
	narrow, err := load(initial.Access(), "capability_requests: {env_names: [A]}")
	if err != nil {
		t.Fatal(err)
	}
	if narrow.Access().CheckEnvName("A") != nil || narrow.Access().CheckEnvName("B") == nil {
		t.Fatal("narrowing failed")
	}
	omitted, err := load(narrow.Access(), "")
	if err != nil || omitted.Access().CheckEnvName("B") == nil {
		t.Fatal("omission restored grant")
	}
	if _, err := load(omitted.Access(), "capability_requests: {env_names: [B]}"); err == nil {
		t.Fatal("reload re-expanded")
	}
	empty, err := load(narrow.Access(), "capability_requests: {env_names: []}")
	if err != nil || empty.Access().CheckEnvName("A") == nil {
		t.Fatal("empty requests must revoke")
	}
	r := narrow.CapabilityRequests()
	r.EnvNames[0] = "B"
	if narrow.CapabilityRequests().EnvNames[0] != "A" || narrow.Access().CheckEnvName("B") == nil {
		t.Fatal("summary mutated stored config authority")
	}
	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{
		"env_names: [HOME]", "overwrite: true", "private_networks: ['10.0.0.0/8']",
		"read_roots: ['/']", "output_root: '/'", "private_networks: [invalid]",
		"env_names: null", "env_names: HOME", "overwrite: 'true'", "env_names: [123]",
	} {
		if _, err := load(denied, "capability_requests: {"+request+"}"); err == nil {
			t.Fatalf("accepted %s", request)
		}
	}
	if _, err := LoadWithPolicy("missing", policy.Access{}); err == nil || !strings.Contains(err.Error(), "config policy") {
		t.Fatal("zero policy did not fail before read")
	}
}

func TestPolicyDiagnosticsDoNotReadOrPrintValues(t *testing.T) {
	secret := fmt.Sprintf("private-%s", filepath.Base(t.TempDir()))
	t.Setenv("POLICY_TEST_TOKEN", secret)
	secondary := writeTempFile(t, "private.txt", secret)
	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		minimalValidYAML + "\ncapability_requests: {env_names: [POLICY_TEST_TOKEN]}",
		minimalValidYAML + fmt.Sprintf("\ntargets_file: %q\ncapability_requests: {overwrite: true}", secondary),
	} {
		_, err := LoadWithPolicy(writeTemp(t, data), denied)
		if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "targets_file:") {
			t.Fatalf("unsafe diagnostic/order: %v", err)
		}
	}
}

func TestPolicyPreflightPreservesDataKeysAndPacingAliases(t *testing.T) {
	for _, data := range []string{
		"targets: [{url: 'https://example.com/{{mode}}/{{policy}}', type: http, weight: 1, vars: {mode: [one], policy: [two]}, http: {headers: {access: value, mode: test}}}]\n",
		"pace: &pace {mode: human}\npacing: *pace\ntargets: [{url: https://example.com, type: http, weight: 1}]\n",
		"defaults: &defaults {pacing: {mode: human}}\n<<: *defaults\ntargets: [{url: https://example.com, type: http, weight: 1}]\n",
	} {
		if _, err := Load(writeTemp(t, data)); err != nil {
			t.Fatalf("legitimate config rejected: %v", err)
		}
	}
}
