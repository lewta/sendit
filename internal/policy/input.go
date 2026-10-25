package policy

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadFile preserves legacy os.ReadFile behavior in trusted mode. Restricted
// reads check a granted path, verify the opened regular-file identity, and
// enforce a hard byte cap before returning any contents.
func ReadFile(access Access, path string, maxBytes int64) ([]byte, error) {
	if access.mode == ModeTrusted && access.allRead {
		return os.ReadFile(path)
	}
	if access.mode != ModeTrusted && access.mode != ModeRestricted {
		return nil, errors.New("config policy: uninitialized access")
	}
	if maxBytes < 1 {
		return nil, errors.New("config policy: input size limit must be positive")
	}
	if err := access.CheckReadPath(path); err != nil {
		return nil, err
	}
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("config policy: input must be a regular file")
	}
	if before.Size() > maxBytes {
		return nil, fmt.Errorf("config policy: input too large (maximum %d bytes)", maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errors.New("config policy: input changed or is not a regular file")
	}
	if err := access.CheckReadPath(path); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("config policy: input too large (maximum %d bytes)", maxBytes)
	}
	return data, nil
}
