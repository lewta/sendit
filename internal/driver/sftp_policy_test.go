package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/policy"
)

func TestSFTPPrivateKeyReadUsesDispatchPolicy(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "id_test")
	if err := os.WriteFile(keyPath, []byte("private-key-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftpPrivateKeyData(policy.WithAccess(t.Context(), denied), keyPath); err == nil ||
		!strings.Contains(err.Error(), "read path denied") || strings.Contains(err.Error(), "private-key-marker") {
		t.Fatalf("denied key read error = %v", err)
	}
	granted, err := policy.New(policy.ModeRestricted, policy.Grants{ReadRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := sftpPrivateKeyData(policy.WithAccess(t.Context(), granted), keyPath)
	if err != nil || string(got) != "private-key-marker" {
		t.Fatalf("granted key read = %q, %v", got, err)
	}
	inline := "-----BEGIN TEST PRIVATE KEY-----\ninline\n"
	got, err = sftpPrivateKeyData(policy.WithAccess(t.Context(), denied), inline)
	if err != nil || string(got) != inline {
		t.Fatalf("inline key = %q, %v", got, err)
	}

	oversized := filepath.Join(root, "oversized-key")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate((1 << 20) + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sftpPrivateKeyData(policy.WithAccess(t.Context(), granted), oversized); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized key error = %v", err)
	}
	if got, err := sftpPrivateKeyData(policy.WithAccess(t.Context(), policy.Trusted()), oversized); err != nil || len(got) != (1<<20)+1 {
		t.Fatalf("trusted oversized key = %d bytes, %v", len(got), err)
	}
}

func TestSFTPKnownHostsReadUsesDispatchPolicy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.Mkdir(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	denied, err := policy.New(policy.ModeRestricted, policy.Grants{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftpHostKeyCallback(policy.WithAccess(t.Context(), denied), config.SFTPConfig{}, map[string]string{}); err == nil ||
		!strings.Contains(err.Error(), "read path denied") {
		t.Fatalf("denied known_hosts error = %v", err)
	}
	granted, err := policy.New(policy.ModeRestricted, policy.Grants{ReadRoots: []string{sshDir}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sftpHostKeyCallback(policy.WithAccess(t.Context(), granted), config.SFTPConfig{}, map[string]string{}); err != nil {
		t.Fatalf("granted known_hosts: %v", err)
	}
}
