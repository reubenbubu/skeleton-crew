// Package compose runs a ticket's compose project through the container
// engine. Every profile's compose.yaml has a service called app, which runs
// the profile's commands.
package compose

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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
// containers, unless the binary is built with the containers tag. Other
// tests give crew a fake Runner.
func Default() Runner {
	return func(dir string, args ...string) ([]byte, int, error) {
		if testing.Testing() && !containersTag {
			return nil, 0, errors.New("refusing to run containers in a test; use a fake")
		}
		engine, err := engine()
		if err != nil {
			return nil, 0, err
		}
		cmd := exec.Command(engine, append([]string{"compose"}, args...)...)
		cmd.Dir = dir
		// Podman otherwise prefixes every output with a banner naming its
		// compose provider.
		cmd.Env = append(os.Environ(), "PODMAN_COMPOSE_WARNING_LOGS=false")
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

// containersTag is set in a build with the containers tag.
var containersTag = false

func engine() (string, error) {
	for _, e := range []string{"podman", "docker"} {
		if exec.Command(e, "compose", "version").Run() == nil {
			return e, nil
		}
	}
	return "", errors.New("no container engine with compose: install podman or docker")
}

// Project is the name of ticket number's compose project in the main
// checkout at root.
func Project(root string, number int) string { return fmt.Sprintf("%s-%d", prefix(root), number) }

// GateProject is the name of the compose project the gate uses for ticket
// number in the main checkout at root, apart from the Dev's own.
func GateProject(root string, number int) string { return Project(root, number) + "-gate" }

// DoctorProject is the name of the compose project crew doctor's gate
// uses in the main checkout at root.
func DoctorProject(root string) string { return prefix(root) + "-doctor" }

// MainProject is the name of the compose project of the main instance of
// the main checkout at root.
func MainProject(root string) string { return prefix(root) + "-main" }

// prefix starts every compose project name of the main checkout at root,
// an absolute path: crew- and the first 8 hex digits of the path's
// SHA-256. Two checkouts on one engine so never share a project, even for
// the same ticket number.
func prefix(root string) string {
	sum := sha256.Sum256([]byte(root))
	return "crew-" + hex.EncodeToString(sum[:4])
}

// Exec runs command, with args appended, in the app container of ticket
// number's compose project in the main checkout at root.
func Exec(run Runner, dir, root string, number int, command string, args []string) ([]byte, int, error) {
	return ExecIn(run, dir, Project(root, number), command, args)
}

// ExecIn runs command, with args appended, in a new app container of the
// compose project called project.
func ExecIn(run Runner, dir, project, command string, args []string) ([]byte, int, error) {
	a := []string{"-p", project, "run", "--rm", "-T", "app", "sh", "-c", command + ` "$@"`, "sh"}
	return run(dir, append(a, args...)...)
}
