//go:build !windows

package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func canonicalPath(path string, missingLeaf bool) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "*?[]\x00") || strings.HasPrefix(path, "//") || strings.Contains(path, `\`) {
		return "", errors.New("config policy: path must be absolute and unambiguous")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "." || part == ".." {
			return "", errors.New("config policy: path traversal components are forbidden")
		}
	}
	clean := filepath.Clean(path)
	p, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return p, nil
	}
	if missingLeaf && os.IsNotExist(err) {
		// A dangling final symlink is not a nonexistent leaf.
		if _, statErr := os.Lstat(clean); os.IsNotExist(statErr) {
			parent, parentErr := filepath.EvalSymlinks(filepath.Dir(clean))
			if parentErr == nil {
				info, statErr := os.Stat(parent)
				if statErr == nil && info.IsDir() {
					return filepath.Join(parent, filepath.Base(clean)), nil
				}
			}
		}
	}
	return "", fmt.Errorf("config policy: cannot resolve path: %s", clean)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
