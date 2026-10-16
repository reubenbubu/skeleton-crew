// Package gate decides whether a commit can merge: the checks that need no
// containers, and the profile's steps in the commit's own compose project.
package gate

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// Untested checks that each of a ticket's criteria has a test that failed
// before it passed: a red log names it, and a test file still names it.
// The full suite passing is the gate's test step. It returns one line per
// kind of gap, or nil.
func Untested(criteria, red []string, named map[string][]string) []string {
	var noRed, noTest []string
	for _, id := range criteria {
		if !slices.Contains(red, id) {
			noRed = append(noRed, id)
		}
		if len(named[id]) == 0 {
			noTest = append(noTest, id)
		}
	}
	var out []string
	if len(noRed) > 0 {
		out = append(out, "tdd: no red log names "+strings.Join(noRed, " "))
	}
	if len(noTest) > 0 {
		out = append(out, "tdd: no test names "+strings.Join(noTest, " "))
	}
	return out
}

var phase = regexp.MustCompile(`^\[(feat|fix|chore):#[0-9]+\] (red|green|refactor): `)

// Behaviour reports whether any of a branch's commit subjects is a red,
// green or refactor step. A branch without one holds only changes with no
// behaviour.
func Behaviour(subjects []string) bool {
	return slices.ContainsFunc(subjects, phase.MatchString)
}

var criterionID = regexp.MustCompile(`#[0-9]+-AC[0-9]+`)

// Named maps each criterion ID that the test files name to those files, in
// path order. files maps each test file's path to its contents.
func Named(files map[string]string) map[string][]string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	named := map[string][]string{}
	for _, p := range paths {
		for _, id := range criterionID.FindAllString(files[p], -1) {
			if !slices.Contains(named[id], p) {
				named[id] = append(named[id], p)
			}
		}
	}
	return named
}

var hunk = regexp.MustCompile(`^@@ -[0-9,]+ \+([0-9]+)`)

// Skipped returns one line for each line that diff, a unified diff, adds
// to a file that isTest accepts and that holds one of markers.
func Skipped(diff string, markers []string, isTest func(path string) bool) []string {
	var out []string
	file, line, header := "", 0, false
	for l := range strings.SplitSeq(diff, "\n") {
		// A file's header is the line after diff --git; the --- and +++
		// lines follow it before the first hunk.
		switch {
		case strings.HasPrefix(l, "diff --git "):
			header = true
		case header && strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
		case header:
			header = !strings.HasPrefix(l, "@@ ")
			if m := hunk.FindStringSubmatch(l); m != nil {
				line, _ = strconv.Atoi(m[1])
			}
		case strings.HasPrefix(l, "@@ "):
			if m := hunk.FindStringSubmatch(l); m != nil {
				line, _ = strconv.Atoi(m[1])
			}
		case strings.HasPrefix(l, "+"):
			for _, m := range markers {
				if !isTest(file) {
					break
				}
				if strings.Contains(l, m) {
					out = append(out, fmt.Sprintf("skipped: %s:%d: %s", file, line, m))
					break
				}
			}
			line++
		case strings.HasPrefix(l, " "):
			line++
		}
	}
	return out
}

// Stale flags each test file that names a criterion that was removed from
// its ticket, belongs to a cancelled ticket, or was revised after the
// file's last change, which changed reports. Flags are in file order.
func Stale(named map[string][]string, tickets map[int]ticket.Known, changed func(file string) (time.Time, error)) ([]string, error) {
	type flag struct{ file, id, why string }
	var flags []flag
	for id, files := range named {
		n, _ := strconv.Atoi(id[1:strings.Index(id, "-")])
		t, ok := tickets[n]
		revised, listed := t.Criteria[id]
		for _, f := range files {
			switch {
			case !ok || !listed:
				flags = append(flags, flag{f, id, "removed"})
			case t.Cancelled:
				flags = append(flags, flag{f, id, "cancelled"})
			case !revised.IsZero():
				at, err := changed(f)
				if err != nil {
					return nil, err
				}
				if at.Before(revised) {
					flags = append(flags, flag{f, id, "revised " + output.Time(revised)})
				}
			}
		}
	}
	slices.SortFunc(flags, func(a, b flag) int {
		if c := strings.Compare(a.file, b.file); c != 0 {
			return c
		}
		return CompareIDs(a.id, b.id)
	})
	var out []string
	for _, f := range flags {
		out = append(out, fmt.Sprintf("flag: %s names %s: %s", f.file, f.id, f.why))
	}
	return out, nil
}

// CompareIDs orders criterion IDs by ticket number, then criterion number.
func CompareIDs(a, b string) int {
	num := func(id string) (int, int) {
		n, k, _ := strings.Cut(id[1:], "-AC")
		x, _ := strconv.Atoi(n)
		y, _ := strconv.Atoi(k)
		return x, y
	}
	an, ak := num(a)
	bn, bk := num(b)
	if an != bn {
		return an - bn
	}
	return ak - bk
}

// Run is the container part of one gate run, in the gated commit's
// worktree at Dir and its own compose project.
type Run struct {
	Compose       compose.Runner
	Clock         clock.Clock
	Poll, Timeout time.Duration // how often, and how long, health is tried
	Dir, Project  string
	Commands      map[string]string // the profile's commands
	Log           io.Writer         // receives every step's output
}

// Failure is the step that failed, why, and its output.
type Failure struct {
	Step, Reason string
	Output       []byte
}

// Steps clears the compose project, so the run starts from a fresh
// database, builds its images and runs the profile's steps, stopping at
// the first that fails. It always takes the project down again. It returns
// an error only when the container engine could not be run at all.
func (r Run) Steps() (*Failure, error) {
	f, err := r.steps()
	if _, _, _, e := r.step("down", "down", "--volumes", "--remove-orphans", "--rmi", "local"); err == nil {
		err = e
	}
	return f, err
}

func (r Run) steps() (*Failure, error) {
	for _, s := range []struct {
		name string
		args []string
	}{{"fresh", []string{"down", "--volumes", "--remove-orphans"}}, {"images", []string{"build"}}} {
		if f, err := r.check(r.step(s.name, s.args...)); f != nil || err != nil {
			return f, err
		}
	}
	for _, name := range []string{"build", "lint", "test"} {
		if cmd, ok := r.Commands[name]; ok {
			if f, err := r.check(r.step(name, "run", "--rm", "-T", "app", "sh", "-c", cmd+` "$@"`, "sh")); f != nil || err != nil {
				return f, err
			}
		}
	}
	health, hasHealth := r.Commands["health"]
	e2e, hasE2E := r.Commands["e2e"]
	if !hasHealth && !hasE2E {
		return nil, nil
	}
	if f, err := r.check(r.step("stack", "up", "-d")); f != nil || err != nil {
		return f, err
	}
	if hasHealth {
		deadline := r.Clock.Now().Add(r.Timeout)
		for {
			name, out, code, err := r.step("health", "exec", "-T", "app", "sh", "-c", health)
			if err != nil || code == 0 {
				if err != nil {
					return nil, err
				}
				break
			}
			if !r.Clock.Now().Before(deadline) {
				return &Failure{Step: name, Reason: fmt.Sprintf("health: not healthy within %s", seconds(r.Timeout)), Output: out}, nil
			}
			<-r.Clock.After(r.Poll)
		}
	}
	if hasE2E {
		return r.check(r.step("e2e", "exec", "-T", "app", "sh", "-c", e2e))
	}
	return nil, nil
}

func seconds(d time.Duration) string { return strconv.Itoa(int(d.Seconds())) + "s" }

// step runs compose with args in the project and logs its output.
func (r Run) step(name string, args ...string) (string, []byte, int, error) {
	fmt.Fprintf(r.Log, "== %s: %s\n", name, strings.Join(args, " "))
	out, code, err := r.Compose(r.Dir, append([]string{"-p", r.Project}, args...)...)
	if err != nil {
		fmt.Fprintf(r.Log, "== %s: %v\n", name, err)
		return name, out, code, err
	}
	r.Log.Write(out)
	fmt.Fprintf(r.Log, "== %s: exit %d\n", name, code)
	return name, out, code, nil
}

// check turns a step's result into a Failure when it did not succeed.
func (r Run) check(name string, out []byte, code int, err error) (*Failure, error) {
	switch {
	case err != nil:
		return nil, err
	case code >= 125 && code <= 127:
		// The engine (125) or the shell (126, 127) failed: the step never ran.
		return &Failure{Step: name, Reason: fmt.Sprintf("%s: could not run: exit %d", name, code), Output: out}, nil
	case code != 0:
		return &Failure{Step: name, Reason: fmt.Sprintf("%s: exit %d", name, code), Output: out}, nil
	}
	return nil, nil
}
