package docker_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type compose struct {
	Services map[string]service `yaml:"services"`
}

type service struct {
	Image       string      `yaml:"image"`
	Environment environment `yaml:"environment"`
	Ports       []any       `yaml:"ports"`
	NetworkMode string      `yaml:"network_mode"`
}

type environment map[string]string

func (e *environment) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		var values map[string]string
		if err := node.Decode(&values); err != nil {
			return err
		}
		*e = values
		return nil
	}
	var values []string
	if err := node.Decode(&values); err != nil {
		return err
	}
	*e = environment{}
	for _, value := range values {
		key, value, _ := strings.Cut(value, "=")
		(*e)[key] = value
	}
	return nil
}

// This policy deliberately accepts only the loopback boundary promised by the
// example, including every published Grafana port, not just port 3000.
func checkPolicy(data []byte, demo, rendered bool) error {
	var cfg compose
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid Compose model")
	}
	g, ok := cfg.Services["grafana"]
	if !ok || len(g.Ports) == 0 || g.NetworkMode != "" {
		return fmt.Errorf("Grafana must publish only loopback ports on the Compose network")
	}
	for _, port := range g.Ports {
		switch p := port.(type) {
		case string:
			if !regexp.MustCompile(`^127\.0\.0\.1:[0-9]+:[0-9]+(/tcp)?$`).MatchString(p) {
				return fmt.Errorf("Grafana published port must bind 127.0.0.1")
			}
		case map[string]any:
			if p["host_ip"] != "127.0.0.1" {
				return fmt.Errorf("rendered Grafana published port must bind 127.0.0.1")
			}
		default:
			return fmt.Errorf("unsupported Grafana published port")
		}
	}
	wantAnonymous := "false"
	if demo {
		wantAnonymous = "true"
	}
	if g.Environment["GF_AUTH_ANONYMOUS_ENABLED"] != wantAnonymous || g.Environment["GF_AUTH_ANONYMOUS_ORG_ROLE"] != "Viewer" {
		return fmt.Errorf("Grafana anonymous policy violated")
	}
	for _, field := range []struct{ key, variable string }{
		{"GF_SECURITY_ADMIN_USER", "GRAFANA_ADMIN_USER"},
		{"GF_SECURITY_ADMIN_PASSWORD", "GRAFANA_ADMIN_PASSWORD"},
	} {
		value := g.Environment[field.key]
		if rendered {
			if value == "" {
				return fmt.Errorf("Grafana authentication input is missing")
			}
		} else if !regexp.MustCompile(`^\$\{` + field.variable + `:\?[^}]+\}$`).MatchString(value) {
			return fmt.Errorf("Grafana authentication must require operator input")
		}
	}
	for name, repository := range map[string]string{"prometheus": "prom/prometheus", "grafana": "grafana/grafana"} {
		if !regexp.MustCompile(`^` + repository + `(:[a-zA-Z0-9_.-]+)?@sha256:[a-f0-9]{64}$`).MatchString(cfg.Services[name].Image) {
			return fmt.Errorf("%s image must use an immutable SHA256 digest", name)
		}
	}
	return nil
}

// Validate local source before permitting the only external operation: Compose
// config. There are no pull/up/run/build or network calls in the policy gate.
func preflight(path string, demo bool, render func() ([]byte, error)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := checkPolicy(data, demo, false); err != nil {
		return err
	}
	data, err = render()
	if err != nil {
		return err
	}
	return checkPolicy(data, demo, true)
}

func renderConfig(t *testing.T, files []string, user, password string) ([]byte, error) {
	t.Helper()
	args := []string{"compose", "--env-file", "/dev/null", "--profile", "observability"}
	for _, file := range files {
		args = append(args, "-f", file)
	}
	args = append(args, "config", "--format", "json")
	cmd := exec.Command("docker", args...)
	dockerConfig := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	// Docker Desktop installs Compose as a user CLI plugin. Preserve plugin
	// discovery without reading the user's Docker config or registry credentials.
	pluginConfig, err := json.Marshal(map[string]any{"cliPluginsExtraDirs": []string{filepath.Join(home, ".docker", "cli-plugins")}})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dockerConfig, "config.json"), pluginConfig, 0600); err != nil {
		return nil, err
	}
	// Do not inherit developer credentials, .env files or Docker contexts. Only
	// placeholders are rendered, and output is never logged (even on failure).
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "DOCKER_CONFIG=" + dockerConfig, "GRAFANA_ADMIN_USER=" + user, "GRAFANA_ADMIN_PASSWORD=" + password}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose config failed (output withheld)")
	}
	return output, nil
}

func TestComposeSecurity(t *testing.T) {
	if err := preflight("docker-compose.yml", false, func() ([]byte, error) {
		return renderConfig(t, []string{"docker-compose.yml"}, "policy-test-user", "policy-test-password")
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("docker-compose.local-anonymous-viewer.yml")
	if err != nil {
		t.Fatal(err)
	}
	var override compose
	if err := yaml.Unmarshal(data, &override); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg compose
	if err := yaml.Unmarshal(base, &cfg); err != nil {
		t.Fatal(err)
	}
	g := cfg.Services["grafana"]
	for key, value := range override.Services["grafana"].Environment {
		g.Environment[key] = value
	}
	if ports := override.Services["grafana"].Ports; ports != nil {
		g.Ports = ports
	}
	cfg.Services["grafana"] = g
	merged, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPolicy(merged, true, false); err != nil {
		t.Fatal(err)
	}
	rendered, err := renderConfig(t, []string{"docker-compose.yml", "docker-compose.local-anonymous-viewer.yml"}, "policy-test-user", "policy-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPolicy(rendered, true, true); err != nil {
		t.Fatal(err)
	}
	for _, inputs := range [][2]string{{"", "policy-test-password"}, {"policy-test-user", ""}, {"", ""}} {
		for _, files := range [][]string{{"docker-compose.yml"}, {"docker-compose.yml", "docker-compose.local-anonymous-viewer.yml"}} {
			if _, err := renderConfig(t, files, inputs[0], inputs[1]); err == nil {
				t.Fatal("Compose accepted missing authentication input")
			}
		}
	}
}

// Independent safe fixture: each mutation names a regression the gate must
// reject, before any renderer/container/network boundary can be reached.
const safeFixture = `services:
  grafana:
    image: grafana/grafana@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "false"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Viewer
      GF_SECURITY_ADMIN_USER: ${GRAFANA_ADMIN_USER:?required}
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_ADMIN_PASSWORD:?required}
    ports: ["127.0.0.1:3000:3000"]
  prometheus:
    image: prom/prometheus@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`

func TestPolicyRejectsUnsafeFixturesBeforeRendering(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		demo           bool
	}{
		{"wildcard", "127.0.0.1:3000", "0.0.0.0:3000", false},
		{"implicit wildcard", "127.0.0.1:3000:3000", "3000:3000", false},
		{"remote", "127.0.0.1:3000", "192.0.2.1:3000", false},
		{"IPv6 wildcard", "127.0.0.1:3000", "[::]:3000", false},
		{"default anonymous", `"false"`, `"true"`, false},
		{"default Admin", "Viewer", "Admin", false},
		{"demo Admin", "Viewer", "Admin", true},
		{"demo Editor", "Viewer", "Editor", true},
		{"demo missing role", "Viewer", `""`, true},
		{"mutable Grafana", "grafana/grafana@sha256:" + strings.Repeat("a", 64), "grafana/grafana:latest", false},
		{"mutable Prometheus", "prom/prometheus@sha256:" + strings.Repeat("b", 64), "prom/prometheus:v3.15.1", false},
		{"missing user", "${GRAFANA_ADMIN_USER:?required}", `""`, false},
		{"missing password", "${GRAFANA_ADMIN_PASSWORD:?required}", `""`, false},
		{"password fallback", "${GRAFANA_ADMIN_PASSWORD:?required}", "${GRAFANA_ADMIN_PASSWORD:-admin}", false},
		{"empty accepted", "${GRAFANA_ADMIN_PASSWORD:?required}", "${GRAFANA_ADMIN_PASSWORD?required}", false},
		{"extra wildcard port", `ports: ["127.0.0.1:3000:3000"]`, `ports: ["127.0.0.1:3000:3000", "3001:3000"]`, false},
		{"host network", `ports: ["127.0.0.1:3000:3000"]`, "network_mode: host\n    ports: [\"127.0.0.1:3000:3000\"]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := safeFixture
			if tc.demo {
				fixture = strings.ReplaceAll(fixture, `"false"`, `"true"`)
			}
			fixture = strings.ReplaceAll(fixture, tc.from, tc.to)
			path := filepath.Join(t.TempDir(), "compose.yml")
			if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
				t.Fatal(err)
			}
			called := false
			err := preflight(path, tc.demo, func() ([]byte, error) {
				called = true
				return nil, fmt.Errorf("unexpected renderer invocation")
			})
			if err == nil || called {
				t.Fatal("unsafe source reached the renderer boundary or was accepted")
			}
		})
	}
}

func TestPolicyRejectsRenderedBoundaryLoss(t *testing.T) {
	for _, port := range []string{`{"target":3000,"published":"3000"}`, `{"host_ip":"0.0.0.0","target":3000,"published":"3000"}`, `{"host_ip":"192.0.2.1","target":3000,"published":"3000"}`} {
		data := strings.ReplaceAll(safeFixture, `ports: ["127.0.0.1:3000:3000"]`, "ports: ["+port+"]")
		data = strings.ReplaceAll(data, "${GRAFANA_ADMIN_USER:?required}", "policy-test-user")
		data = strings.ReplaceAll(data, "${GRAFANA_ADMIN_PASSWORD:?required}", "policy-test-password")
		if err := checkPolicy([]byte(data), false, true); err == nil {
			t.Fatal("accepted rendered port without loopback boundary")
		}
	}
}

func TestPolicyAcceptsSafeFixtures(t *testing.T) {
	for _, demo := range []bool{false, true} {
		fixture := safeFixture
		if demo {
			fixture = strings.ReplaceAll(fixture, `"false"`, `"true"`)
		}
		if err := checkPolicy([]byte(fixture), demo, false); err != nil {
			t.Fatal(err)
		}
	}
}
