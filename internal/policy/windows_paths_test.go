package policy

import (
	"errors"
	"os"
	"testing"
)

func TestWindowsPathSyntax(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`C:\`, `C:\`}, {`c:\Data\input.txt`, `C:\Data\input.txt`},
		{`c:/Data/input.txt`, `C:\Data\input.txt`}, {`C:\Data\`, `C:\Data`},
		{`\\server\share`, `\\server\share`}, {`\\Server\Share\Data\`, `\\Server\Share\Data`},
		{`//Server/Share/Data/leaf`, `\\Server\Share\Data\leaf`},
		{`C:\資料\input.txt`, `C:\資料\input.txt`},
	} {
		got, err := windowsPathSyntax(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, path := range []string{
		``, `.`, `relative`, `C:relative`, `C:`, `\rooted`, `/rooted`, `1:\Data`,
		`\\server`, `\\server\`, `\\\server\share`, `\\server\\share`, `C:\Data\\leaf`,
		`\\?\C:\Data`, `\\?\UNC\server\share`, `\\.\C:\Data`, `\??\C:\Data`, `\\??\C:\Data`,
		`C:\Data\..\outside`, `C:\Data\.\leaf`, `\\server\share\..\outside`,
		`C:\Data\leaf:stream`, `C:\Data\leaf.`, `C:\Data\leaf `, `C:\Data\*`, `C:\Data\a?`, `C:\Data\[a]`,
		`C:\Data\NUL`, `C:\Data\nul.txt`, `C:\Data\NUL .txt`, `C:\Data\CON`, `C:\Data\LPT1.log`, `C:\Data\COM¹.txt`, `C:\Data\CONOUT$`,
		"C:\\Data\\a\x00b", "C:\\Data\\a\x01b", `C:\Data\a|b`, `C:\Data\a"b`,
	} {
		if _, err := windowsPathSyntax(path); err == nil {
			t.Errorf("ambiguous Windows path accepted: %q", path)
		}
	}
}

// Components supplied here model already-resolved OS canonical names, not raw
// caller spelling. Exact component comparison keeps case-sensitive directories
// distinct. Drive/UNC volume spelling may differ in ASCII case.
func TestWindowsCanonicalContainment(t *testing.T) {
	for _, tc := range []struct {
		root, path string
		want       bool
	}{
		{`C:\Data`, `c:\Data\leaf`, true}, {`C:\`, `C:\Data`, true},
		{`C:\Data`, `C:\Data`, true}, {`C:\Data`, `C:\DataOther\leaf`, false},
		{`C:\Data`, `D:\Data\leaf`, false}, {`C:\Data`, `C:\Data\..\outside`, false},
		{`C:\Data`, `C:\data\leaf`, false},
		{`\\Server\Share\Data`, `\\server\share\Data\leaf`, true},
		{`\\server\share`, `\\SERVER\SHARE\Data`, true},
		{`\\server\share\Data`, `\\server\share2\Data\leaf`, false},
		{`\\server\share\Data`, `\\elsewhere\share\Data\leaf`, false},
		{`\\server\share\Data`, `\\server\share\DataElse\leaf`, false},
		{`C:\Data`, `\\server\share\Data\leaf`, false},
		{`C:\Data`, `\\?\C:\Data\leaf`, false},
	} {
		if got := windowsWithin(tc.root, tc.path); got != tc.want {
			t.Errorf("%q in %q: %v, want %v", tc.path, tc.root, got, tc.want)
		}
	}
}

func TestWindowsFinalPathNamespaces(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`\\?\C:\Data\leaf`, `C:\Data\leaf`},
		{`\\?\UNC\server\share\Data\leaf`, `\\server\share\Data\leaf`},
	} {
		got, err := windowsFinalPath(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("final path %q: %q, %v", tc.input, got, err)
		}
	}
	for _, path := range []string{`\\?\GLOBALROOT\Device\Harddisk0\Data`, `\\?\Volume{abcd}\Data`, `\\.\pipe\name`, `\Device\HarddiskVolume1\Data`, `\\?\C:\Data\leaf:stream`} {
		if _, err := windowsFinalPath(path); err == nil {
			t.Errorf("device final path accepted: %q", path)
		}
	}
}

// Only the unavailable Windows metadata boundary is faked. This executes the
// same resolution/missing-leaf logic used by the Windows build, and checks its
// result against literal containment expectations.
func TestWindowsResolvedPathsAndMissingLeaves(t *testing.T) {
	for _, tc := range []struct {
		name, path, final, parent, parentFinal           string
		missingLeaf, absent, parentDir, dangling, denied bool
		want                                             string
		inside                                           bool
	}{
		{name: "normal", path: `C:\Data\input`, final: `\\?\C:\Data\input`, want: `C:\Data\input`, inside: true},
		{name: "junction escape", path: `C:\Data\link\input`, final: `\\?\D:\Other\input`, want: `D:\Other\input`},
		{name: "symlink in root", path: `C:\Data\link`, final: `\\?\C:\Data\input`, want: `C:\Data\input`, inside: true},
		{name: "missing output", path: `C:\Data\new`, missingLeaf: true, absent: true, parent: `C:\Data`, parentFinal: `\\?\C:\Data`, parentDir: true, want: `C:\Data\new`, inside: true},
		{name: "missing under junction", path: `C:\Data\link\new`, missingLeaf: true, absent: true, parent: `C:\Data\link`, parentFinal: `\\?\UNC\server\share\Other`, parentDir: true, want: `\\server\share\Other\new`},
		{name: "missing read", path: `C:\Data\new`, absent: true},
		{name: "missing parent", path: `C:\Data\missing\new`, missingLeaf: true, absent: true, parent: `C:\Data\missing`},
		{name: "parent file", path: `C:\Data\file\new`, missingLeaf: true, absent: true, parent: `C:\Data\file`, parentFinal: `\\?\C:\Data\file`},
		{name: "dangling link", path: `C:\Data\link`, missingLeaf: true, absent: true, dangling: true, parentFinal: `\\?\C:\Data`, parentDir: true},
		{name: "access denied", path: `C:\Data\input`, missingLeaf: true, denied: true, parentFinal: `\\?\C:\Data`, parentDir: true},
		{name: "device final", path: `C:\Data\link`, final: `\\?\GLOBALROOT\Device\Harddisk0\input`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			resolve := func(path string) (string, bool, error) {
				calls++
				if calls == 1 {
					if path != tc.path {
						t.Fatalf("resolved wrong path: %q", path)
					}
					if tc.denied {
						return "", false, os.ErrPermission
					}
					if tc.absent {
						return "", false, os.ErrNotExist
					}
					return tc.final, false, nil
				}
				if tc.parent == "" || path != tc.parent || calls != 2 {
					t.Fatalf("unexpected parent resolution: %q", path)
				}
				if tc.parentFinal == "" {
					return "", false, os.ErrNotExist
				}
				return tc.parentFinal, tc.parentDir, nil
			}
			exists := func(path string) error {
				if path != tc.path || !tc.missingLeaf || !tc.absent {
					t.Fatalf("unexpected no-follow existence check: %q", path)
				}
				if tc.dangling {
					return nil
				}
				return os.ErrNotExist
			}
			got, err := windowsResolvePath(tc.path, tc.missingLeaf, resolve, exists)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("unexpected resolution: %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			if inside := windowsWithin(`C:\Data`, got); inside != tc.inside {
				t.Fatalf("containment: %v, want %v", inside, tc.inside)
			}
		})
	}
	called := false
	_, err := windowsResolvePath(`\\?\C:\Data`, true, func(string) (string, bool, error) {
		called = true
		return "", false, errors.New("must not resolve")
	}, func(string) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("ambiguous namespace reached metadata boundary")
	}
}
