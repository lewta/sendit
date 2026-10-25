package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func restrictedOutputAccess(t *testing.T, root string) Access {
	t.Helper()
	overwrite := true
	access, err := New(ModeRestricted, Grants{OutputRoot: &root, Overwrite: &overwrite})
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func TestPreparedOutputRechecksAliasesBeforeCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output")
	if err := os.WriteFile(path, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareOutput(restrictedOutputAccess(t, root), path, false)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Link(path, alias); err != nil {
		_ = prepared.Abort()
		t.Skipf("hard links unavailable: %v", err)
	}
	if file, err := prepared.Commit(); err == nil {
		_ = file.Close()
		t.Fatal("hard link added after prepare was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "preserve" {
		t.Fatalf("failed commit modified output: %q, %v", got, err)
	}
}

func TestPreparedOutputRechecksPathIdentityBeforeCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output")
	moved := filepath.Join(root, "moved")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareOutput(restrictedOutputAccess(t, root), path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, moved); err != nil {
		_ = prepared.Abort()
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		_ = prepared.Abort()
		t.Fatal(err)
	}
	if file, err := prepared.Commit(); err == nil {
		_ = file.Close()
		t.Fatal("path replacement after prepare was accepted")
	}
	for file, want := range map[string]string{moved: "original", path: "replacement"} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", file, got, err, want)
		}
	}
}
