//go:build windows

package policy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsNativeGrantsAndCase(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "Input.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := mustAccess(t, ModeRestricted, Grants{ReadRoots: []string{root}, OutputRoot: ptr(root)})
	if err := a.CheckReadPath(file); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckOutputPath(filepath.Join(root, "new.txt"), false); err != nil {
		t.Fatal(err)
	}
	if a.CheckOutputPath(filepath.Join(root, "missing", "new.txt"), false) == nil {
		t.Fatal("missing parent accepted")
	}
	if a.CheckOutputPath(root, false) == nil {
		t.Fatal("output root itself accepted as a filename")
	}
	// Case-insensitive volumes accept equivalent caller spelling. A filesystem
	// configured for case-sensitive directories may reject that spelling itself.
	if _, err := os.Stat(strings.ToLower(file)); err == nil {
		if err := a.CheckReadPath(strings.ToLower(file)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWindowsNativeJunctionEscape(t *testing.T) {
	base := t.TempDir()
	root, outside := filepath.Join(base, "granted"), filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "input"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	// Only literal relative names reach cmd; the temp path is supplied as a
	// process working directory, never interpolated into shell command text.
	cmd := exec.Command("cmd", "/d", "/c", "mklink", "/J", `granted\junction`, "outside")
	cmd.Dir = base
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creating local junction fixture: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	a := mustAccess(t, ModeRestricted, Grants{ReadRoots: []string{root}, OutputRoot: ptr(root)})
	if a.CheckReadPath(filepath.Join(junction, "input")) == nil {
		t.Fatal("junction read escaped root")
	}
	if a.CheckOutputPath(filepath.Join(junction, "new"), false) == nil {
		t.Fatal("junction output escaped root")
	}
	// An operator can deliberately grant the resolved junction target itself.
	b := mustAccess(t, ModeRestricted, Grants{ReadRoots: []string{junction}})
	if err := b.CheckReadPath(filepath.Join(junction, "input")); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNativeSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink fixture requires Windows developer mode or privilege: %v", err)
	}
	a := mustAccess(t, ModeRestricted, Grants{ReadRoots: []string{root}, OutputRoot: ptr(root)})
	if a.CheckOutputPath(filepath.Join(link, "new"), false) == nil {
		t.Fatal("symlink output escaped root")
	}
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(outside, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if a.CheckOutputPath(dangling, false) == nil {
		t.Fatal("dangling symlink treated as nonexistent output leaf")
	}
}
