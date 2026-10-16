package github_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

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
	dir := t.TempDir()
	stub := filepath.Join(dir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$0.args"
cat > "$0.stdin"
printf 'HTTP/2.0 201 Created\nContent-Type: application/json; charset=utf-8\r\nX-Github-Request-Id: AB:CD\r\n\r\n{"number":7,"title":"t","node_id":"I_kwDOx"}'
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := github.New("user/project", github.Exec(stub))
	got, err := c.CreateIssue(github.NewIssue{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != 7 || got.Title != "t" {
		t.Fatalf("issue %+v", got)
	}
	args, _ := os.ReadFile(stub + ".args")
	if want := "api\n--include\n--method\nPOST\nrepos/user/project/issues\n--input\n-\n"; string(args) != want {
		t.Fatalf("args %q, want %q", args, want)
	}
	if stdin, _ := os.ReadFile(stub + ".stdin"); string(stdin) != `{"title":"t","body":""}` {
		t.Fatalf("stdin %q", stdin)
	}
}

// #3-AC4: gh exits 1 on an HTTP error but still prints the response.
func TestExecReadsErrorResponse(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "gh")
	script := `#!/bin/sh
printf 'HTTP/2.0 404 Not Found\nContent-Type: application/json\r\n\r\n{"message":"Not Found"}'
echo 'gh: Not Found (HTTP 404)' >&2
exit 1
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := github.New("user/project", github.Exec(stub)).Issue(9)
	if want := "unexpected github response: GET repos/user/project/issues/9: HTTP 404 Not Found"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
