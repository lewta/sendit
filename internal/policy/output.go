package policy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PreparedOutput is an opened, verified destination that has not yet truncated
// an existing file. Abort must be called unless Commit transfers ownership.
type PreparedOutput struct {
	file      *os.File
	path      string
	access    Access
	created   bool
	truncate  bool
	committed bool
	info      os.FileInfo
}

// PrepareExplicitOutput hardens a direct command-line destination. Supplying
// the path is the operator's authority to create or replace that exact file;
// the restricted open still rejects symlinks, hard links, and path races.
func PrepareExplicitOutput(path string, appendMode bool) (*PreparedOutput, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(absolute)
	overwrite := true
	access, err := New(ModeRestricted, Grants{OutputRoot: &root, Overwrite: &overwrite})
	if err != nil {
		return nil, err
	}
	return PrepareOutput(access, absolute, appendMode)
}

// PrepareOutput opens and verifies an output without truncating existing data.
// Restricted mode rejects final symlinks and multi-link inodes so a permitted
// pathname cannot mutate a second name outside the granted root.
func PrepareOutput(access Access, path string, appendMode bool) (*PreparedOutput, error) {
	if access.Mode() != ModeTrusted && access.Mode() != ModeRestricted {
		return nil, errors.New("config policy: uninitialized access")
	}
	entry, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := access.CheckOutputPath(path, false); err != nil {
			return nil, err
		}
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if openErr != nil {
			return nil, openErr
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			prepared := &PreparedOutput{file: f, path: path, access: access, created: true, info: info}
			return nil, errors.Join(errors.New("config policy: output must be a regular file"), statErr, prepared.Abort())
		}
		return &PreparedOutput{file: f, path: path, access: access, created: true, info: info}, nil
	}
	if err != nil {
		return nil, err
	}
	if access.Mode() == ModeRestricted && entry.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("config policy: output symlinks are denied")
	}
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("config policy: output must be a regular file")
	}
	truncate := !appendMode
	if err := access.CheckOutputPath(path, truncate); err != nil {
		return nil, err
	}
	flags := os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		_ = f.Close()
		return nil, errors.Join(errors.New("config policy: output changed or is not a regular file"), err)
	}
	if access.Mode() == ModeRestricted {
		links, linkErr := outputLinkCount(f, after)
		if linkErr != nil {
			_ = f.Close()
			return nil, fmt.Errorf("config policy: checking output links: %w", linkErr)
		}
		if links != 1 {
			_ = f.Close()
			return nil, errors.New("config policy: output hard links are denied")
		}
	}
	if err := access.CheckOutputPath(path, truncate); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &PreparedOutput{file: f, path: path, access: access, truncate: truncate, info: after}, nil
}

// SameFile reports whether two prepared destinations name the same inode.
func (p *PreparedOutput) SameFile(other *PreparedOutput) bool {
	return p != nil && other != nil && p.info != nil && other.info != nil && os.SameFile(p.info, other.info)
}

// SameInfo reports whether a previously opened file refers to this prepared
// destination. It is used to reject input/output aliases before truncation.
func (p *PreparedOutput) SameInfo(info os.FileInfo) bool {
	return p != nil && p.info != nil && info != nil && os.SameFile(p.info, info)
}

// Mode reports the verified opened destination's file mode.
func (p *PreparedOutput) Mode() os.FileMode {
	if p == nil || p.info == nil {
		return 0
	}
	return p.info.Mode()
}

// Commit applies delayed truncation and transfers the open file to the caller.
func (p *PreparedOutput) Commit() (*os.File, error) {
	if p == nil || p.file == nil || p.committed {
		return nil, errors.New("config policy: invalid prepared output")
	}
	if err := p.verifyRestricted(); err != nil {
		return nil, errors.Join(err, p.Abort())
	}
	if p.truncate {
		if err := p.file.Truncate(0); err != nil {
			return nil, errors.Join(err, p.Abort())
		}
		if _, err := p.file.Seek(0, io.SeekStart); err != nil {
			return nil, errors.Join(err, p.Abort())
		}
	}
	p.committed = true
	return p.file, nil
}

func (p *PreparedOutput) verifyRestricted() error {
	if p.access.Mode() != ModeRestricted {
		return nil
	}
	entry, err := os.Lstat(p.path)
	if err != nil {
		return err
	}
	if entry.Mode()&os.ModeSymlink != 0 {
		return errors.New("config policy: output symlinks are denied")
	}
	current, err := os.Stat(p.path)
	if err != nil {
		return err
	}
	opened, err := p.file.Stat()
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(current, opened) {
		return errors.New("config policy: output changed or is not a regular file")
	}
	links, err := outputLinkCount(p.file, opened)
	if err != nil {
		return fmt.Errorf("config policy: checking output links: %w", err)
	}
	if links != 1 {
		return errors.New("config policy: output hard links are denied")
	}
	if err := p.access.CheckOutputPath(p.path, p.truncate); err != nil {
		return err
	}
	p.info = opened
	return nil
}

// Abort closes an uncommitted destination and removes only a newly-created file
// that still refers to the inode this preparation created.
func (p *PreparedOutput) Abort() error {
	if p == nil || p.file == nil || p.committed {
		return nil
	}
	closeErr := p.file.Close()
	if !p.created {
		return closeErr
	}
	current, err := os.Stat(p.path)
	if err == nil && p.info != nil && os.SameFile(current, p.info) {
		err = os.Remove(p.path)
	}
	if os.IsNotExist(err) {
		err = nil
	}
	return errors.Join(closeErr, err)
}
