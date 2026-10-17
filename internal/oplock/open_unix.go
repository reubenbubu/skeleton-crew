//go:build unix

package oplock

import (
	"errors"
	"os"
	"syscall"
)

// open opens path and takes an exclusive flock on it, which the kernel
// drops when the process exits.
func open(path string) (*os.File, bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, &os.PathError{Op: "flock", Path: path, Err: err}
	}
	return f, true, nil
}
