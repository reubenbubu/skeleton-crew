package oplock

import (
	"errors"
	"os"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION.
const errSharingViolation syscall.Errno = 32

// open opens path for writing, sharing it with readers only, so a second
// open for writing fails until this handle closes. Windows closes it when
// the process exits.
func open(path string) (*os.File, bool, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ,
		nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, errSharingViolation) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), true, nil
}
