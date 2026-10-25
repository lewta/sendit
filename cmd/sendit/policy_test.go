package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/policy"
	"github.com/spf13/cobra"
)

const policyConfig = "targets: [{url: https://example.com, type: http, weight: 1}]\n"

func writePolicyConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A missing path distinguishes policy validation from attempting a config read.
func TestCommandsRejectInvalidPolicyBeforeConfigRead(t *testing.T) {
	for _, factory := range []func() *cobra.Command{startCmd, validateCmd} {
		for _, flags := range [][]string{
			{"--config-trust", "bad"}, {"--allow-private-network", "bad"},
			{"--config-trust", "restricted", "--allow-env", "*"},
			{"--allow-read-root", "relative"}, {"--allow-output-root", "relative"},
			{"--allow-env", ""}, {"--allow-output-root", ""},
		} {
			cmd := factory()
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetArgs(append([]string{"--config", filepath.Join(t.TempDir(), "missing")}, flags...))
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "config policy") || strings.Contains(err.Error(), "reading config") {
				t.Fatalf("%s %v: %v", cmd.Name(), flags, err)
			}
		}
	}
}

func TestSharedPolicyBuilderExactGrants(t *testing.T) {
	dir := t.TempDir()
	for _, factory := range []func() *cobra.Command{startCmd, validateCmd} {
		cmd := factory()
		var out, warnings bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&warnings)
		path := writePolicyConfig(t, policyConfig+fmt.Sprintf("capability_requests: {env_names: [A, B], read_roots: [%q], output_root: %q, overwrite: true, private_networks: ['10.1.0.0/16']}\n", dir, dir))
		args := []string{"--config", path, "--config-trust", "restricted", "--allow-env", "A", "--allow-env", "B", "--allow-read-root", dir, "--allow-output-root", dir, "--allow-overwrite", "--allow-private-network", "10.0.0.0/8"}
		if cmd.Name() == "start" {
			args = append(args, "--dry-run")
		}
		cmd.SetArgs(args)
		var err error
		_ = captureStdout(t, func() { err = cmd.Execute() })
		if err != nil {
			t.Fatal(err)
		}
		if got := out.String(); !strings.Contains(got, "restricted") || !strings.Contains(got, "requested=") || !strings.Contains(got, "granted=") || !strings.Contains(got, "10.1.0.0/16") {
			t.Fatalf("missing summary: %s", got)
		}
		if warnings.Len() != 0 {
			t.Fatalf("restricted warning: %s", &warnings)
		}
	}
}

func TestPolicyCommandDiagnosticsHideValues(t *testing.T) {
	secret := fmt.Sprintf("private-%s", filepath.Base(t.TempDir()))
	t.Setenv("POLICY_TEST_TOKEN", secret)
	for _, factory := range []func() *cobra.Command{startCmd, validateCmd} {
		for _, mode := range []string{"trusted", "restricted"} {
			cmd := factory()
			var out, warnings bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&warnings)
			path := writePolicyConfig(t, "targets: [{url: https://example.com, type: http, weight: 1, auth: {type: bearer, token_env: POLICY_TEST_TOKEN}}]\n")
			args := []string{"--config", path}
			if mode == "restricted" {
				args = append(args, "--config-trust", mode, "--allow-env", "POLICY_TEST_TOKEN")
			}
			if cmd.Name() == "start" {
				args = append(args, "--dry-run")
			}
			cmd.SetArgs(args)
			var err error
			legacy := captureStdout(t, func() { err = cmd.Execute() })
			if err != nil {
				t.Fatal(err)
			}
			combined := out.String() + warnings.String() + legacy
			if strings.Contains(combined, secret) || !strings.Contains(out.String(), mode) {
				t.Fatalf("unsafe/missing summary: %s", combined)
			}
			wantWarnings := 0
			if mode == "trusted" {
				wantWarnings = 1
			}
			if strings.Count(warnings.String(), "trusted config") != wantWarnings {
				t.Fatalf("warning count: %s", warnings.String())
			}
		}
	}
}

func TestReloadPolicyUsesLastAcceptedCeiling(t *testing.T) {
	root := t.TempDir()
	access, err := policy.New(policy.ModeRestricted, policy.Grants{EnvNames: []string{"A", "B"}, ReadRoots: []string{root}, PrivateNetworks: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	if err != nil {
		t.Fatal(err)
	}
	path := writePolicyConfig(t, policyConfig)
	accepted := 0
	accept := func(cfg *config.Config) error { accepted++; return nil }
	write := func(request string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(policyConfig+request), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("capability_requests: {env_names: [A]}\n")
	access, err = reloadConfig(path, access, accept)
	if err != nil || access.CheckEnvName("B") == nil {
		t.Fatalf("narrow reload: %v", err)
	}
	write("")
	access, err = reloadConfig(path, access, accept)
	if err != nil || access.CheckEnvName("B") == nil {
		t.Fatal("omission restored authority")
	}
	for _, attempt := range []string{"capability_requests: {env_names: [B]}\n", "policy: {mode: trusted}\n", "config_trust: trusted\n"} {
		write(attempt)
		access, err = reloadConfig(path, access, accept)
		if err == nil || access.CheckEnvName("B") == nil || accepted != 2 {
			t.Fatal("malicious reload reached engine or widened access")
		}
	}
	write("capability_requests: {env_names: []}\n")
	access, err = reloadConfig(path, access, func(*config.Config) error { return errors.New("engine rejected replacement") })
	if err == nil || access.CheckEnvName("A") != nil {
		t.Fatal("failed reload changed ceiling")
	}
	access, err = reloadConfig(path, access, accept)
	if err != nil || access.CheckEnvName("A") == nil {
		t.Fatal("accepted reload did not remove grant")
	}
}

func TestReloadConfigPreservesCaptureOverrideAndRejectsItsRevocation(t *testing.T) {
	root := t.TempDir()
	capture := filepath.Join(root, "capture.pcap")
	access, err := policy.New(policy.ModeRestricted, policy.Grants{OutputRoot: &root})
	if err != nil {
		t.Fatal(err)
	}
	path := writePolicyConfig(t, policyConfig)
	initial, err := config.LoadWithPolicy(path, access, config.WithPCAPOverride(capture))
	if err != nil {
		t.Fatal(err)
	}
	if initial.Output.PCAPFile != capture {
		t.Fatalf("initial capture = %q", initial.Output.PCAPFile)
	}
	if err := os.WriteFile(capture, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	accepted := 0
	accept := func(next *config.Config) error {
		accepted++
		if next.Output != initial.Output {
			return fmt.Errorf("capture override lost: %+v", next.Output)
		}
		return nil
	}
	reloadOptions := []config.LoadOption{config.WithPCAPOverride(capture), config.WithActiveOutput(initial.Output)}
	current, err := reloadConfig(path, initial.Access(), accept, reloadOptions...)
	if err != nil || accepted != 1 {
		t.Fatalf("unchanged capture reload: accepted=%d err=%v", accepted, err)
	}

	data := policyConfig + "capability_requests: {output_root: ''}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := reloadConfig(path, current, accept, reloadOptions...)
	if err == nil || !strings.Contains(err.Error(), "output path denied") || accepted != 1 {
		t.Fatalf("capture revocation: accepted=%d err=%v", accepted, err)
	}
	if err := after.CheckOutputPath(capture, false); err != nil {
		t.Fatalf("rejected reload changed ceiling: %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil || string(got) != "preserve" {
		t.Fatalf("reload modified capture: %q, %v", got, err)
	}

	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadWithPolicy(writePolicyConfig(t, policyConfig), denied, config.WithPCAPOverride(filepath.Join(t.TempDir(), "denied.pcap"))); err == nil || !strings.Contains(err.Error(), "output path denied") {
		t.Fatalf("initial capture bypassed policy: %v", err)
	}
}

func TestPolicyRejectionPrecedesStartupEffects(t *testing.T) {
	dir := t.TempDir()
	output, pid := filepath.Join(dir, "output.jsonl"), filepath.Join(dir, "sendit.pid")
	secret := fmt.Sprintf("private-%s", filepath.Base(dir))
	if err := os.WriteFile(output, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writePolicyConfig(t, policyConfig+fmt.Sprintf("pacing: {mode: scheduled, schedule: [{cron: '0 0 31 2 *', duration_minutes: 1, requests_per_minute: 1}]}\noutput: {enabled: true, file: %q}\ndaemon: {pid_file: %q}\nconfig_trust: trusted\n", output, pid))
	cmd := startCmd()
	var messages bytes.Buffer
	cmd.SetOut(&messages)
	cmd.SetErr(&messages)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"--config", path, "--config-trust", "restricted", "--duration", "10ms"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "config policy") {
		t.Fatalf("expected preflight rejection: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != secret {
		t.Fatal("output modified before rejection")
	}
	if _, err := os.Stat(pid); !os.IsNotExist(err) {
		t.Fatal("PID created before rejection")
	}
	if strings.Contains(messages.String(), secret) || strings.Contains(messages.String(), "engine started") {
		t.Fatal("unexpected startup/secret diagnostic")
	}
}

func TestOperatorDenialWinsOverYAML(t *testing.T) {
	for _, factory := range []func() *cobra.Command{startCmd, validateCmd} {
		cmd := factory()
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		path := writePolicyConfig(t, policyConfig+"capability_requests: {overwrite: true}\n")
		cmd.SetArgs([]string{"--config", path, "--allow-overwrite=false"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "overwrite denied") {
			t.Fatalf("operator denial lost: %v", err)
		}
	}
}
