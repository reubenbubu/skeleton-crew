// Package events keeps the lead DM's local event state: when crew events
// last read, and which crew events --wait is the current one. Both are
// local files in the main checkout that are never committed.
package events

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/toml"
)

const (
	// ReadPath records when crew events last read, relative to the root
	// of the main checkout.
	ReadPath = ".crew/events.read"
	// WaitPath names the current wait. A wait that finds another name
	// there has been replaced.
	WaitPath = ".crew/events.wait"
)

// Read returns when events were last read in root. ok is false when there
// is no record. An unreadable record counts as none, and warning says why.
func Read(root string) (at time.Time, ok bool, warning string) {
	s, err := value(root, ReadPath, "read")
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, ""
	}
	if err == nil {
		at, err = time.Parse(time.RFC3339, s)
	}
	if err != nil && !errors.Is(err, errUnreadable) {
		err = errors.New("invalid read time")
	}
	if err != nil {
		return time.Time{}, false, "warning: " + ReadPath + " unreadable: " + err.Error() + "; counted as no record"
	}
	return at.UTC(), true, ""
}

// MarkRead records in root that events were read up to at.
func MarkRead(root string, at time.Time) error {
	return write(root, ReadPath, "read", at.UTC().Format(time.RFC3339))
}

// Begin makes a new wait the current one in root, which replaces any other,
// and returns its name.
func Begin(root string) (string, error) {
	b := make([]byte, 16)
	rand.Read(b)
	name := hex.EncodeToString(b)
	return name, write(root, WaitPath, "wait", name)
}

// Current reports whether the wait called name is still the current one in
// root.
func Current(root, name string) bool {
	s, err := value(root, WaitPath, "wait")
	return err == nil && s == name
}

var errUnreadable = errors.New("cannot read")

// value reads the one string key of the file at rel in root.
func value(root, rel, key string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", errUnreadable, err)
	}
	doc, err := toml.Parse(rel, b)
	v, ok := doc.Values[key]
	if err != nil || !ok || len(doc.Values) != 1 || len(doc.Tables) > 0 || v.Kind != toml.String {
		return "", errors.New("invalid " + key)
	}
	return v.Str, nil
}

// write replaces the file at rel in root with one key, by renaming a
// temporary file into place, so no reader sees it half written.
func write(root, rel, key, s string) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = fmt.Fprintf(tmp, "%s = %q\n", key, s)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
