//go:build windows

package policy

import (
	"errors"
	"os"
	"slices"
	"strings"

	"golang.org/x/sys/windows"
)

func canonicalPath(path string, missingLeaf bool) (string, error) {
	return windowsResolvePath(path, missingLeaf, windowsMetadataPath, func(path string) error {
		_, err := os.Lstat(path)
		return err
	})
}

func within(root, path string) bool { return windowsWithin(root, path) }

// windowsMetadataPath follows all filesystem reparse points, including
// junctions, via the kernel. EvalSymlinks alone is insufficient: newer Go
// versions do not mark every name-surrogate reparse point as ModeSymlink.
// OPEN_EXISTING and desired access 0 obtain metadata without reading contents
// or creating/truncating anything. FILE_NAME_NORMALIZED (0) returns the actual
// canonical component names; containment must not case-fold those components.
// https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfinalpathnamebyhandlew
func windowsMetadataPath(path string) (string, bool, error) {
	// Conversion to the extended namespace happens only after strict lexical
	// validation. It supports long native paths without accepting caller-supplied
	// device/extended namespaces or changing Win32 path interpretation.
	path, err := windowsPathSyntax(path)
	if err != nil {
		return "", false, err
	}
	if strings.HasPrefix(path, `\\`) {
		path = `\\?\UNC\` + path[2:]
	} else {
		path = `\\?\` + path
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false, err
	}
	handle, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false, err
	}
	defer windows.CloseHandle(handle) //nolint:errcheck // Read-only metadata handle.
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", false, err
	}
	var buffer [32768]uint16
	n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", false, err
	}
	if n == 0 || n >= uint32(len(buffer)) {
		return "", false, errors.New("config policy: unsupported Windows final-path length")
	}
	final := windows.UTF16ToString(buffer[:n])
	encoded, err := windows.UTF16FromString(final)
	if err != nil || !slices.Equal(encoded[:len(encoded)-1], buffer[:n]) {
		return "", false, errors.New("config policy: invalid Windows final-path encoding")
	}
	return final, info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0, nil
}
