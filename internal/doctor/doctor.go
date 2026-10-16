// Package doctor checks everything that would stop the crew: the tools,
// gh's login, the labels, the configuration, the project knowledge and the
// harness wiring. It never handles a credential: for a missing login it
// names the command the user runs in their own terminal.
package doctor

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// The statuses of a check. A warning does not fail crew doctor.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
)

// Check is the result of one check.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Passed returns a check that passed.
func Passed(name, detail string) Check { return Check{Name: name, Status: OK, Detail: detail} }

// Warning returns a check that warns.
func Warning(name, detail string) Check { return Check{Name: name, Status: Warn, Detail: detail} }

// Failure returns a check that failed.
func Failure(name, detail string) Check { return Check{Name: name, Status: Fail, Detail: detail} }

// String formats c as one line, such as "ok git 2.47.1",
// "warn knowledge: ..." or "fail gh: not installed".
func (c Check) String() string {
	if c.Status == OK {
		return strings.TrimSpace("ok " + c.Name + " " + c.Detail)
	}
	return c.Status + " " + c.Name + ": " + c.Detail
}

// Auth and Engine name the checks of gh's login and of the container
// engine when none is installed.
const (
	Auth   = github.Command + "-auth"
	Engine = "container-engine"
)

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
		if c.Status == Fail {
			return true
		}
	}
	return false
}

// Run runs the checks of the tools and gh's login, in a fixed order.
func Run(probe Probe, gh github.Runner) []Check {
	checks := []Check{gitCheck(probe), engineCheck(probe)}
	v, err := github.Version(gh)
	if err != nil {
		return append(checks, Failure(github.Command, detail(err)))
	}
	checks = append(checks, Passed(github.Command, v))
	u, err := github.New("", gh).User()
	switch {
	case github.AuthRequired(err):
		return append(checks, Failure(Auth, "run in your own terminal: gh auth login"))
	case err != nil:
		return append(checks, Failure(Auth, err.Error()))
	}
	return append(checks, Passed(Auth, u.Login))
}

func gitCheck(probe Probe) Check {
	out, err := probe("git", "--version")
	if err != nil {
		return Failure("git", notInstalled(err))
	}
	// git version 2.47.1
	return Passed("git", field(out, 2))
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
			return Passed(engine, strings.TrimSuffix(field(out, 2), ",")+" compose")
		}
	}
	if first != "" {
		return Failure(first, "compose is not available")
	}
	return Failure(Engine, "install podman or docker, with compose")
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
