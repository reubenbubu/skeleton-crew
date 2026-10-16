// Package lead records which DM session holds the lead, in a local lock
// file that is never committed.
package lead

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/toml"
)

// Path is the lock file, relative to the root of the main checkout.
const Path = ".crew/lead.lock"

// Lead is the session that holds the lead: its token and when it claimed it.
type Lead struct {
	Token   string    `json:"token"`
	Claimed time.Time `json:"claimed"`
}

// HeldError refuses a claim while another session holds the lead.
type HeldError struct{ Claimed time.Time }

func (e *HeldError) Error() string {
	return "lead held since " + output.Time(e.Claimed) + "; crew dm claim --takeover takes it once the user confirms"
}

// NotLeadError refuses a lead-only command from a session without the lead.
// Claimed is zero when no session holds the lead.
type NotLeadError struct{ Claimed time.Time }

func (e *NotLeadError) Error() string {
	if e.Claimed.IsZero() {
		return "not-lead: no lead; run crew dm claim"
	}
	return "not-lead: the lead was claimed at " + output.Time(e.Claimed) + " by another session"
}

var token = regexp.MustCompile(`^[0-9a-f]{32}$`)

// read reads the lock in root. A missing lock is no lead. An unreadable one
// is no lead too, and warning says why.
func read(root string) (l *Lead, warning string, err error) {
	b, err := os.ReadFile(filepath.Join(root, Path))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, "", nil
	case err != nil:
		return nil, unreadable(err.Error()), nil
	}
	l, why := parse(b)
	if l == nil {
		return nil, unreadable(why), nil
	}
	return l, "", nil
}

func unreadable(why string) string {
	return "warning: " + Path + " unreadable: " + why + "; counted as no lead"
}

// parse parses a lock, or says why it cannot.
func parse(b []byte) (*Lead, string) {
	doc, err := toml.Parse(Path, b)
	if err != nil {
		return nil, err.Error()
	}
	keys := make([]string, 0, len(doc.Values))
	for k := range doc.Values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"claimed", "token"}) || len(doc.Tables) > 0 {
		return nil, "want only token and claimed"
	}
	t, c := doc.Values["token"], doc.Values["claimed"]
	if t.Kind != toml.String || !token.MatchString(t.Str) {
		return nil, "invalid token"
	}
	at, err := time.Parse(time.RFC3339, c.Str)
	if c.Kind != toml.String || err != nil {
		return nil, "invalid claimed time"
	}
	return &Lead{Token: t.Str, Claimed: at.UTC()}, ""
}

// Claim gives the lead in root to a new token, claimed at now. Unless
// takeover is true, it is refused with a HeldError while another session
// holds the lead. warning is set when an unreadable lock was counted as no
// lead.
func Claim(root string, now time.Time, takeover bool) (l Lead, warning string, err error) {
	cur, warning, err := read(root)
	if err != nil {
		return Lead{}, warning, err
	}
	if cur != nil && !takeover {
		return Lead{}, warning, &HeldError{cur.Claimed}
	}
	b := make([]byte, 16)
	rand.Read(b)
	l = Lead{Token: hex.EncodeToString(b), Claimed: now.UTC().Truncate(time.Second)}
	path := filepath.Join(root, Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Lead{}, warning, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "lead.lock.*")
	if err != nil {
		return Lead{}, warning, err
	}
	defer os.Remove(tmp.Name())
	_, err = fmt.Fprintf(tmp, "token = %q\nclaimed = %q\n", l.Token, l.Claimed.Format(time.RFC3339))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Lead{}, warning, err
	}
	if takeover {
		// A rename replaces any lock at once, so the old token stops
		// working with no moment of no lead. A repair guard left by a
		// claim that died goes with it.
		if err := os.Rename(tmp.Name(), path); err != nil {
			return Lead{}, warning, err
		}
		os.Remove(path + repairSuffix)
		return l, warning, nil
	}
	if warning != "" {
		// Only one claim at a time replaces an unreadable lock, and it
		// reads the lock again first, so a claim that read the same
		// unreadable lock earlier cannot remove the winner's.
		g, err := os.OpenFile(path+repairSuffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			return Lead{}, warning, errChanged
		} else if err != nil {
			return Lead{}, warning, err
		}
		g.Close()
		defer os.Remove(path + repairSuffix)
		if cur, _, _ := read(root); cur != nil {
			return Lead{}, warning, &HeldError{cur.Claimed}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Lead{}, warning, err
		}
	}
	// A link fails if a lock appeared since the read, so of two claims at
	// the same moment only one wins, and the lock is never seen half
	// written.
	if err := os.Link(tmp.Name(), path); errors.Is(err, fs.ErrExist) {
		if cur, _, _ := read(root); cur != nil {
			return Lead{}, warning, &HeldError{cur.Claimed}
		}
		return Lead{}, warning, errChanged
	} else if err != nil {
		return Lead{}, warning, err
	}
	return l, warning, nil
}

// repairSuffix names the guard that a claim holds while it replaces an
// unreadable lock.
const repairSuffix = ".repair"

var errChanged = errors.New("lead lock changed during the claim; run crew dm claim again, or crew dm claim --takeover once the user confirms")

// Check refuses with a NotLeadError unless tok is the lead's token in root.
// warning is set when an unreadable lock was counted as no lead.
func Check(root, tok string) (warning string, err error) {
	cur, warning, err := read(root)
	if err != nil {
		return warning, err
	}
	if cur == nil {
		return warning, &NotLeadError{}
	}
	if cur.Token != tok {
		return warning, &NotLeadError{cur.Claimed}
	}
	return warning, nil
}

// Release gives up the lead in root, which tok must hold.
func Release(root, tok string) (warning string, err error) {
	if warning, err = Check(root, tok); err != nil {
		return warning, err
	}
	// The lock moves aside before its token is checked again, so a
	// takeover since the check is put back, not removed.
	path := filepath.Join(root, Path)
	aside := fmt.Sprintf("%s.release.%d", path, os.Getpid())
	if err := os.Rename(path, aside); errors.Is(err, fs.ErrNotExist) {
		return warning, nil
	} else if err != nil {
		return warning, err
	}
	defer os.Remove(aside)
	b, err := os.ReadFile(aside)
	if err != nil {
		return warning, err
	}
	if cur, _ := parse(b); cur != nil && cur.Token != tok {
		os.Link(aside, path)
		return warning, &NotLeadError{cur.Claimed}
	}
	return warning, nil
}
