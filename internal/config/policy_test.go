package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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

func TestLoadWithPolicyTrustedCompatibility(t *testing.T) {
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
