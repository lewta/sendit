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
	return linkCountUint64(stat.Nlink), nil
}

// Stat_t.Nlink varies in width and signedness across Unix targets (for example,
// uint64 on Linux, uint16 on Darwin, and int16 on AIX). Keeping the conversion
// generic makes it meaningful on every supported host.
func linkCountUint64[T ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64](count T) uint64 {
	return uint64(count)
}
