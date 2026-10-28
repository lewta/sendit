//go:build windows

package pidfile

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func ensureDefaultDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("creating private PID directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspecting private PID directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private PID directory is not a real directory")
	}
	descriptor, err := currentUserSecurityDescriptor(true)
	if err != nil {
		return err
	}
	return secureWindowsDirectory(path, descriptor)
}

func createLocked(path string) (*os.File, error) {
	file, err := openWindows(path, windows.CREATE_NEW)
	if err != nil {
		return nil, err
	}
	if err := lockWindows(file, true); err != nil {
		cleanupCreatedFile(file, path)
		return nil, err
	}
	return file, nil
}

func openValidated(path string) (*os.File, error) {
	return openWindows(path, windows.OPEN_EXISTING)
}

func openWindows(path string, disposition uint32) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	desiredAccess := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	var attributes *windows.SecurityAttributes
	if disposition == windows.CREATE_NEW {
		descriptor, descriptorErr := currentUserSecurityDescriptor(false)
		if descriptorErr != nil {
			return nil, descriptorErr
		}
		attributes = &windows.SecurityAttributes{
			Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
			SecurityDescriptor: descriptor,
		}
	}
	handle, err := windows.CreateFile(
		name,
		desiredAccess,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		attributes,
		disposition,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	cleanupFailure := func() {
		if disposition == windows.CREATE_NEW {
			_ = removeOwnedFile(file, path)
		}
		_ = file.Close()
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		cleanupFailure()
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		cleanupFailure()
		return nil, errors.New("PID record is a reparse point")
	}
	if info.NumberOfLinks != 1 {
		cleanupFailure()
		return nil, errors.New("PID record has multiple hard links")
	}
	if err := validateWindowsSecurity(handle, false); err != nil {
		cleanupFailure()
		return nil, err
	}
	return file, nil
}

func validateWindowsSecurity(handle windows.Handle, directory bool) error {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("reading PID record security: %w", err)
	}
	expected, err := currentUserSecurityDescriptor(directory)
	if err != nil {
		return err
	}
	return validateWindowsDescriptor(descriptor, expected)
}

func validateWindowsDescriptor(actual, expected *windows.SECURITY_DESCRIPTOR) error {
	actualOwner, actualOwnerDefaulted, err := actual.Owner()
	if err != nil {
		return fmt.Errorf("reading PID record owner: %w", err)
	}
	expectedOwner, _, err := expected.Owner()
	if err != nil {
		return fmt.Errorf("reading expected PID record owner: %w", err)
	}
	if actualOwnerDefaulted || actualOwner == nil || expectedOwner == nil || !actualOwner.Equals(expectedOwner) {
		return errors.New("PID record has unsafe Windows owner")
	}
	control, _, err := actual.Control()
	if err != nil {
		return fmt.Errorf("reading PID record security control: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return errors.New("PID record Windows access controls inherit from a parent")
	}
	actualDACL, actualDACLDefaulted, err := actual.DACL()
	if err != nil {
		return fmt.Errorf("reading PID record access controls: %w", err)
	}
	expectedDACL, _, err := expected.DACL()
	if err != nil {
		return fmt.Errorf("reading expected PID record access controls: %w", err)
	}
	if actualDACLDefaulted || !equalWindowsACL(actualDACL, expectedDACL) {
		return errors.New("PID record has unsafe Windows access controls")
	}
	return nil
}

func equalWindowsACL(actual, expected *windows.ACL) bool {
	if actual == nil || expected == nil || actual.AceCount != expected.AceCount {
		return false
	}
	matched := make([]bool, actual.AceCount)
	for expectedIndex := uint16(0); expectedIndex < expected.AceCount; expectedIndex++ {
		var expectedACE *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(expected, uint32(expectedIndex), &expectedACE) != nil || expectedACE == nil || expectedACE.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		expectedSID := (*windows.SID)(unsafe.Pointer(&expectedACE.SidStart))
		found := false
		for actualIndex := uint16(0); actualIndex < actual.AceCount; actualIndex++ {
			if matched[actualIndex] {
				continue
			}
			var actualACE *windows.ACCESS_ALLOWED_ACE
			if windows.GetAce(actual, uint32(actualIndex), &actualACE) != nil || actualACE == nil || actualACE.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
				return false
			}
			actualSID := (*windows.SID)(unsafe.Pointer(&actualACE.SidStart))
			if actualACE.Mask == expectedACE.Mask && actualACE.Header.AceFlags == expectedACE.Header.AceFlags && actualSID.Equals(expectedSID) {
				matched[actualIndex] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func currentUserSecurityDescriptor(directory bool) (*windows.SECURITY_DESCRIPTOR, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, fmt.Errorf("reading current process token: %w", err)
	}
	defer token.Close() //nolint:errcheck
	current, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("reading current process user: %w", err)
	}
	if current == nil || current.User.Sid == nil {
		return nil, errors.New("reading current process user: missing SID")
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	sid := current.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString(
		fmt.Sprintf("O:%sD:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)", sid, flags, sid, flags),
	)
	if err != nil {
		return nil, fmt.Errorf("building private PID security descriptor: %w", err)
	}
	return descriptor, nil
}

func secureWindowsDirectory(path string, expected *windows.SECURITY_DESCRIPTOR) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.WRITE_DAC|windows.WRITE_OWNER,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fmt.Errorf("opening private PID directory: %w", err)
	}
	defer windows.CloseHandle(handle) //nolint:errcheck
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("inspecting private PID directory: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("private PID directory is a reparse point")
	}
	owner, _, err := expected.Owner()
	if err != nil {
		return fmt.Errorf("reading private PID directory owner policy: %w", err)
	}
	dacl, _, err := expected.DACL()
	if err != nil {
		return fmt.Errorf("reading private PID directory access policy: %w", err)
	}
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("securing private PID directory: %w", err)
	}
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("reading private PID directory security: %w", err)
	}
	if err := validateWindowsDescriptor(descriptor, expected); err != nil {
		return fmt.Errorf("validating private PID directory security: %w", err)
	}
	return nil
}

func lockWindows(file *os.File, exclusive bool) error {
	overlapped := windows.Overlapped{Offset: uint32(lockByteOffset)}
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	return windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped)
}

func lockedByOwner(file *os.File) (bool, error) {
	err := lockWindows(file, false)
	if err == nil {
		if unlockErr := unlock(file); unlockErr != nil {
			return false, unlockErr
		}
		return false, nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	return false, err
}

func unlock(file *os.File) error {
	overlapped := windows.Overlapped{Offset: uint32(lockByteOffset)}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}

func removeOwnedFile(file *os.File, path string) error {
	ownerInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspecting owned PID record: %w", err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	deleteHandle, err := windows.CreateFile(
		name,
		windows.DELETE|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return err
	}
	deleteFile := os.NewFile(uintptr(deleteHandle), path)
	defer deleteFile.Close() //nolint:errcheck
	deleteInfo, err := deleteFile.Stat()
	if err != nil {
		return fmt.Errorf("inspecting PID record before deletion: %w", err)
	}
	if !os.SameFile(ownerInfo, deleteInfo) {
		return errors.New("PID path changed before deletion")
	}
	var handleInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(deleteHandle, &handleInfo); err != nil {
		return fmt.Errorf("inspecting PID record links before deletion: %w", err)
	}
	if handleInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || handleInfo.NumberOfLinks != 1 {
		return errors.New("PID path became unsafe before deletion")
	}
	deleteOnClose := byte(1)
	return windows.SetFileInformationByHandle(
		deleteHandle,
		windows.FileDispositionInfo,
		&deleteOnClose,
		1,
	)
}
