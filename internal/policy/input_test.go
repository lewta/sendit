package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileRestrictedBoundsDoNotChangeTrustedCompatibility(t *testing.T) {
	root := t.TempDir()
	large := filepath.Join(root, "large-input")
	content := strings.Repeat("x", 1025)
	if err := os.WriteFile(large, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile(Trusted(), large, 1024)
	if err != nil || string(got) != content {
		t.Fatalf("trusted read = %d bytes, %v", len(got), err)
	}
	restricted, err := New(ModeRestricted, Grants{ReadRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(restricted, large, 1024); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("restricted oversized read error = %v", err)
	}
	trustedNarrowed, err := New(ModeTrusted, Grants{ReadRoots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(trustedNarrowed, large, 1024); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("explicitly narrowed trusted read error = %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err = ReadFile(Trusted(), link, 1)
	if err != nil || string(got) != "outside-marker" {
		t.Fatalf("trusted symlink read = %q, %v", got, err)
	}
	if _, err := ReadFile(restricted, link, 1024); err == nil || !strings.Contains(err.Error(), "read path denied") {
		t.Fatalf("restricted symlink escape error = %v", err)
	}
	if _, err := ReadFile(trustedNarrowed, link, 1024); err == nil || !strings.Contains(err.Error(), "read path denied") {
		t.Fatalf("explicitly narrowed trusted symlink error = %v", err)
	}
}
