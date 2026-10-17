package github_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// stubEnv names the response a test binary started as gh gives, and
// stubOut the file prefix it records its arguments and stdin at.
const (
	stubEnv = "CREW_TEST_GH_STUB"
	stubOut = "CREW_TEST_GH_STUB_OUT"
)

// TestMain lets the test binary stand in for gh, so the stub runs on every
// platform without a shell.
func TestMain(m *testing.M) {
	switch os.Getenv(stubEnv) {
	case "":
		os.Exit(m.Run())
	case "created":
		out := os.Getenv(stubOut)
		os.WriteFile(out+".args", []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o644)
		stdin, _ := io.ReadAll(os.Stdin)
		os.WriteFile(out+".stdin", stdin, 0o644)
		fmt.Print("HTTP/2.0 201 Created\nContent-Type: application/json; charset=utf-8\r\nX-Github-Request-Id: AB:CD\r\n\r\n{\"number\":7,\"title\":\"t\",\"node_id\":\"I_kwDOx\"}")
	case "not-found":
		fmt.Print("HTTP/2.0 404 Not Found\nContent-Type: application/json\r\n\r\n{\"message\":\"Not Found\"}")
		fmt.Fprintln(os.Stderr, "gh: Not Found (HTTP 404)")
		os.Exit(1)
	}
	os.Exit(0)
}

// stub copies the test binary into a temporary directory as gh, to answer
// with response.
func stub(t *testing.T, response string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(stubEnv, response)
	t.Setenv(stubOut, path)
	return path
}

// #3-AC3
func TestExecRefusesRealGhInTests(t *testing.T) {
	for _, path := range []string{"gh", "/usr/bin/gh"} {
		_, err := github.Exec(path)([]string{"api", "user"}, nil)
		if err == nil || !strings.Contains(err.Error(), "refusing to run") {
			t.Fatalf("%s: got %v, want a refusal", path, err)
		}
	}
}

// #3-AC1: the exec runner passes arguments and stdin to gh and reads gh's
// real --include output, with its mixed line endings.
func TestExecRunsGh(t *testing.T) {
	gh := stub(t, "created")
	c := github.New("user/project", github.Exec(gh))
	got, err := c.CreateIssue(github.NewIssue{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 7 || got.Title != "t" {
		t.Fatalf("issue %+v", got)
	}
	args, _ := os.ReadFile(gh + ".args")
	if want := "api\n--include\n--method\nPOST\nrepos/user/project/issues\n--input\n-\n"; string(args) != want {
		t.Fatalf("args %q, want %q", args, want)
	}
	if stdin, _ := os.ReadFile(gh + ".stdin"); string(stdin) != `{"title":"t","body":""}` {
		t.Fatalf("stdin %q", stdin)
	}
}

// #3-AC4: gh exits 1 on an HTTP error but still prints the response.
func TestExecReadsErrorResponse(t *testing.T) {
	_, err := github.New("user/project", github.Exec(stub(t, "not-found"))).Issue(9)
	if want := "unexpected github response: GET repos/user/project/issues/9: HTTP 404 Not Found"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
