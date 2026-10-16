package github

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Result is what one run of gh produced.
type Result struct {
	Stdout, Stderr []byte
	Code           int
}

// Runner runs gh with args, feeding it stdin. It returns an error only when
// gh could not be run at all; a non-zero exit is reported in Result.Code.
type Runner func(args []string, stdin []byte) (Result, error)

// Command is the name of the gh executable.
const Command = "gh"

// Default is the Runner that runs gh from the PATH.
func Default() Runner { return Exec(Command) }

// Exec returns a Runner that runs the gh executable at path, normally "gh".
//
// In a test binary it refuses any gh outside the temporary directory, so an
// automated test can never reach a real GitHub host. Tests use the fake in
// package fakegithub, or a stub script they write to a temporary directory.
func Exec(path string) Runner {
	return func(args []string, stdin []byte) (Result, error) {
		if testing.Testing() && !inTempDir(path) {
			return Result{}, fmt.Errorf("refusing to run %s in a test; use a fake", path)
		}
		cmd := exec.Command(path, args...)
		cmd.Stdin = bytes.NewReader(stdin)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
		err := cmd.Run()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			return Result{}, err
		}
		return Result{Stdout: out.Bytes(), Stderr: errOut.Bytes(), Code: cmd.ProcessState.ExitCode()}, nil
	}
}

func inTempDir(path string) bool {
	tmp := filepath.Clean(os.TempDir()) + string(filepath.Separator)
	return filepath.IsAbs(path) && strings.HasPrefix(filepath.Clean(path), tmp)
}
