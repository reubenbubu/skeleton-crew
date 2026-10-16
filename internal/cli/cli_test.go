package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	env := Env{Version: "v1.2.3", Stdout: &out, Stderr: &errOut, Clock: clock.NewFake(clock.Epoch)}
	code = Run(args, env)
	return code, out.String(), errOut.String()
}

// #2-AC1
func TestVersionPrintsEmbeddedVersion(t *testing.T) {
	code, out, errOut := run("--version")
	if code != 0 || out != "v1.2.3\n" || errOut != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	outputtest.CheckTerse(t, out)
}

// #2-AC4
func TestVersionJSON(t *testing.T) {
	for _, args := range [][]string{{"--version", "--json"}, {"--json", "--version"}} {
		code, out, errOut := run(args...)
		if code != 0 || out != `{"version":"v1.2.3"}`+"\n" || errOut != "" {
			t.Fatalf("%v: got code=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
}

// #2-AC6
func TestUnknownCommand(t *testing.T) {
	code, out, errOut := run("bogus", "--json")
	if code == 0 || out != "" || errOut != "unknown command: bogus\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// #2-AC6
func TestUnknownFlag(t *testing.T) {
	for _, flag := range []string{"--bogus", "-x", "--bogus=1"} {
		code, out, errOut := run(flag)
		if code == 0 || out != "" || errOut != "unknown flag: "+flag+"\n" {
			t.Fatalf("%s: got code=%d stdout=%q stderr=%q", flag, code, out, errOut)
		}
	}
}

// #2-AC6
func TestNoCommand(t *testing.T) {
	code, out, errOut := run()
	if code == 0 || out != "" || errOut != "usage: crew <command>\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// #2-AC4
func TestJSONWriteFailureExitsNonZero(t *testing.T) {
	env := Env{Version: "v1.2.3", Stdout: failingWriter{}, Stderr: &bytes.Buffer{}, Clock: clock.NewFake(clock.Epoch)}
	if code := Run([]string{"--version", "--json"}, env); code == 0 {
		t.Fatal("exit 0 after a failed write")
	}
}
