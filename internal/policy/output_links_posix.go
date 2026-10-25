//go:build !windows

package policy

import (
	"errors"
	"os"
	"syscall"
)

func outputLinkCount(_ *os.File, info os.FileInfo) (uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("unsupported file metadata")
	}
	return uint64(stat.Nlink), nil
}
