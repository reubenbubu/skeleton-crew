// Package compose runs a ticket's compose project through the container
// engine. Every profile's compose.yaml has a service called app, which runs
// the profile's commands.
package compose

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

// Runner runs `<engine> compose <args>` in dir and returns its combined
// output and exit code. It returns an error only when the engine could not
// be run at all.
type Runner func(dir string, args ...string) (out []byte, code int, err error)

// Default is the Runner that uses the first of podman and docker that has
// compose, as crew doctor reports.
//
// In a test binary it refuses to run, so an automated test never starts
// containers. Tests give crew a fake Runner.
func Default() Runner {
	return func(dir string, args ...string) ([]byte, int, error) {
		if testing.Testing() {
			return nil, 0, errors.New("refusing to run containers in a test; use a fake")
		}
		engine, err := engine()
		if err != nil {
			return nil, 0, err
		}
		cmd := exec.Command(engine, append([]string{"compose"}, args...)...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err = cmd.Run()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			return nil, 0, err
		}
		return out.Bytes(), cmd.ProcessState.ExitCode(), nil
	}
}

func engine() (string, error) {
	for _, e := range []string{"podman", "docker"} {
		if exec.Command(e, "compose", "version").Run() == nil {
			return e, nil
		}
	}
	return "", errors.New("no container engine with compose: install podman or docker")
}

// Project is the name of ticket number's compose project.
func Project(number int) string { return fmt.Sprintf("crew-%d", number) }

// Exec runs command, with args appended, in the app container of ticket
// number's compose project.
func Exec(run Runner, dir string, number int, command string, args []string) ([]byte, int, error) {
	a := []string{"-p", Project(number), "run", "--rm", "-T", "app", "sh", "-c", command + ` "$@"`, "sh"}
	return run(dir, append(a, args...)...)
}
