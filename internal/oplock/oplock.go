// Package oplock is the lock that lets one lead-only command at a time act
// in a checkout. The operating system holds it for the process that took
// it, and releases it when that process exits, however it exits, so it is
// never left behind.
package oplock

import (
	"os"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/sharedfile"
)

// Lock is a held lock.
type Lock struct{ f *os.File }

// TryLock takes the lock at path and writes holder into it. It returns
// false, and no error, when another Lock holds it.
func TryLock(path, holder string) (*Lock, bool, error) {
	f, ok, err := open(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	if err := f.Truncate(0); err == nil {
		_, err = f.WriteAt([]byte(holder+"\n"), 0)
	}
	if err != nil {
		f.Close()
		return nil, false, err
	}
	return &Lock{f}, true, nil
}

// Release gives the lock up.
func (l *Lock) Release() error { return l.f.Close() }

// Holder returns what the current holder of the lock at path wrote, or ""
// when it cannot be read.
func Holder(path string) string {
	b, err := sharedfile.Read(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
