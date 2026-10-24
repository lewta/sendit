package policy

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// windowsPathSyntax is host-independent so the Windows lexical contract is
// tested on every platform. It accepts absolute drive and UNC paths only.
// It does not collapse traversal, empty components, device names, ADS, or
// trailing dot/space aliases into an apparently safe path.
func windowsPathSyntax(path string) (string, error) {
	invalid := errors.New("config policy: Windows path must use an unambiguous absolute drive or UNC path")
	if !utf8.ValidString(path) {
		return "", invalid
	}
	path = strings.ReplaceAll(path, "/", `\`)
	var volume, tail string
	switch {
	case len(path) >= 3 && asciiLetter(path[0]) && path[1:3] == `:\`:
		volume, tail = asciiUpper(path[:2]), path[3:]
	case strings.HasPrefix(path, `\\`):
		parts := strings.Split(path[2:], `\`)
		if len(parts) < 2 || !windowsComponent(parts[0]) || !windowsComponent(parts[1]) {
			return "", invalid
		}
		volume = `\\` + parts[0] + `\` + parts[1]
		tail = strings.Join(parts[2:], `\`)
	default:
		return "", invalid
	}
	if tail != "" {
		tail = strings.TrimSuffix(tail, `\`)
		for _, part := range strings.Split(tail, `\`) {
			if !windowsComponent(part) {
				return "", invalid
			}
		}
		return volume + `\` + tail, nil
	}
	if len(volume) == 2 {
		return volume + `\`, nil
	}
	return volume, nil
}

func windowsComponent(part string) bool {
	if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, `<>:"/\|?*[]`) {
		return false
	}
	for _, r := range part {
		if r < 32 {
			return false
		}
	}
	base, _, _ := strings.Cut(part, ".")
	base = asciiUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "CLOCK$":
		return false
	}
	if strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT") {
		switch base[3:] {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
			return false
		}
	}
	return true
}

func asciiLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

func asciiUpper(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}, s)
}

// windowsWithin compares OS-normalized paths, whose existing components have
// their actual filesystem spelling. Do not case-fold directory components:
// NTFS and remote filesystems may have case-sensitive directories. Drive/UNC
// volumes compare in ASCII case only, avoiding Unicode-fold namespace aliases.
func windowsWithin(root, path string) bool {
	r, err := windowsPathSyntax(root)
	if err != nil {
		return false
	}
	p, err := windowsPathSyntax(path)
	if err != nil {
		return false
	}
	rv, rp := windowsVolumeAndTail(r)
	pv, pp := windowsVolumeAndTail(p)
	return asciiUpper(rv) == asciiUpper(pv) && (rp == "" || pp == rp || strings.HasPrefix(pp, rp+`\`))
}

// Inputs to this helper have already passed windowsPathSyntax.
func windowsVolumeAndTail(path string) (string, string) {
	if path[1] == ':' {
		return path[:2], path[3:]
	}
	parts := strings.SplitN(path[2:], `\`, 3)
	volume := `\\` + parts[0] + `\` + parts[1]
	if len(parts) == 2 {
		return volume, ""
	}
	return volume, parts[2]
}

// Only the native metadata resolver may supply extended paths here. Callers'
// device/extended namespace paths are rejected by windowsPathSyntax before I/O.
func windowsFinalPath(path string) (string, error) {
	switch {
	case len(path) >= 8 && asciiUpper(path[:8]) == `\\?\UNC\`:
		path = `\\` + path[8:]
	case strings.HasPrefix(path, `\\?\`):
		path = path[4:]
	default:
		return "", errors.New("config policy: unsupported Windows final-path namespace")
	}
	return windowsPathSyntax(path)
}

// windowsResolvePath contains the common, host-testable resolution protocol.
// resolve returns the OS final path and directory bit from one metadata handle;
// exists must examine the final component without following a reparse point.
func windowsResolvePath(path string, missingLeaf bool, resolve func(string) (string, bool, error), exists func(string) error) (string, error) {
	clean, err := windowsPathSyntax(path)
	if err != nil {
		return "", err
	}
	final, _, err := resolve(clean)
	if err == nil {
		return windowsFinalPath(final)
	}
	if missingLeaf && os.IsNotExist(err) && os.IsNotExist(exists(clean)) {
		volume, tail := windowsVolumeAndTail(clean)
		if tail != "" {
			parent, leaf := volume, tail
			if i := strings.LastIndex(tail, `\`); i >= 0 {
				parent, leaf = volume+`\`+tail[:i], tail[i+1:]
			}
			if len(parent) == 2 {
				parent += `\`
			}
			finalParent, isDir, parentErr := resolve(parent)
			if parentErr == nil && isDir {
				canonicalParent, parentErr := windowsFinalPath(finalParent)
				if parentErr == nil {
					return windowsPathSyntax(strings.TrimSuffix(canonicalParent, `\`) + `\` + leaf)
				}
			}
		}
	}
	return "", fmt.Errorf("config policy: cannot resolve path: %s", clean)
}
