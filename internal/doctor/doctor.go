// Package doctor checks that the tools crew needs are installed and that gh
// is logged in. It never handles a credential: for a missing login it names
// the command the user runs in their own terminal.
package doctor

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// Check is the result of one check.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// String formats c as one line, such as "ok git 2.47.1" or
// "fail gh: not installed".
func (c Check) String() string {
	if c.OK {
		return strings.TrimSpace("ok " + c.Name + " " + c.Detail)
	}
	return "fail " + c.Name + ": " + c.Detail
}

// Probe runs a local tool and returns its standard output. An error that
// wraps exec.ErrNotFound means the tool is not installed.
type Probe func(name string, args ...string) (string, error)

// Exec is the Probe that runs the real tools.
func Exec(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// Failed reports whether any check failed.
func Failed(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return true
		}
	}
	return false
}

// Run runs every check, in a fixed order.
func Run(probe Probe, gh github.Runner) []Check {
	checks := []Check{gitCheck(probe), engineCheck(probe)}
	v, err := github.Version(gh)
	if err != nil {
		return append(checks, Check{Name: github.Command, Detail: detail(err)})
	}
	checks = append(checks, Check{Name: github.Command, OK: true, Detail: v})
	u, err := github.New("", gh).User()
	switch {
	case github.AuthRequired(err):
		return append(checks, Check{Name: github.Command + "-auth", Detail: "run in your own terminal: gh auth login"})
	case err != nil:
		return append(checks, Check{Name: github.Command + "-auth", Detail: err.Error()})
	}
	return append(checks, Check{Name: github.Command + "-auth", OK: true, Detail: u.Login})
}

func gitCheck(probe Probe) Check {
	out, err := probe("git", "--version")
	if err != nil {
		return Check{Name: "git", Detail: notInstalled(err)}
	}
	// git version 2.47.1
	return Check{Name: "git", OK: true, Detail: field(out, 2)}
}

// engineCheck picks the first of podman and docker that has compose.
func engineCheck(probe Probe) Check {
	var first string
	for _, engine := range []string{"podman", "docker"} {
		out, err := probe(engine, "--version")
		if err != nil {
			continue
		}
		if first == "" {
			first = engine
		}
		if _, err := probe(engine, "compose", "version"); err == nil {
			// podman version 5.7.0, or Docker version 27.3.1, build ce12230
			return Check{Name: engine, OK: true, Detail: strings.TrimSuffix(field(out, 2), ",") + " compose"}
		}
	}
	if first != "" {
		return Check{Name: first, Detail: "compose is not available"}
	}
	return Check{Name: "container-engine", Detail: "install podman or docker, with compose"}
}

func notInstalled(err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		return "not installed"
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}

func detail(err error) string {
	var e *github.Error
	if errors.As(err, &e) && e.Status == 0 {
		return e.Detail
	}
	return err.Error()
}

// field returns the nth word of the first line of out, or "" if there is
// none.
func field(out string, n int) string {
	line, _, _ := strings.Cut(out, "\n")
	f := strings.Fields(line)
	if n < len(f) {
		return f[n]
	}
	return ""
}
