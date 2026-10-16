package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/github"
)

const worktree = ".crew/worktrees/feat/1/login-with-email"

// containers stands in for the container engine's compose. It records each
// call and answers with out and code.
type containers struct {
	calls []string // the directory, then the arguments
	out   string
	code  int
}

func (c *containers) run(dir string, args ...string) ([]byte, int, error) {
	c.calls = append(c.calls, dir+" "+strings.Join(args, " "))
	return []byte(c.out), c.code, nil
}

// devProject is a project with crew.toml, a lead and ticket 1, Login with
// email, at 3:eligible.
type devProject struct {
	*project
	token string
	main  string // the main checkout
	box   *containers
}

func newDevProject(t *testing.T) *devProject {
	t.Helper()
	p := &devProject{project: newTicketProject(t), box: &containers{}}
	p.compose = p.box.run
	p.main = p.dir
	p.write(".crew/crew.toml", string(config.Template("python", "")))
	p.token = p.claim()
	p.ok("ticket", "new", "Login with email")
	p.setBody(1, "## Goal\nLog in.\n## Acceptance criteria\n- [#1-AC1] A known email gets a link.\n")
	p.ok("ticket", "ready", "1")
	p.fake.SetBranch("main", p.git("rev-parse", "HEAD"))
	return p
}

func (p *devProject) start(n int, args ...string) string {
	p.t.Helper()
	return p.ok(append([]string{"ticket", "start", fmt.Sprint(n), "--lead", p.token}, args...)...)
}

// inWorktree starts ticket 1 at rank 2 and makes the Dev's worktree the
// directory crew runs in.
func (p *devProject) inWorktree() {
	p.t.Helper()
	p.start(1, "--tier", "2")
	p.dir = filepath.Join(p.main, worktree)
	p.fake.SetBranch("feat/1/login-with-email", "")
}

// failed runs crew and fails the test unless it exits with code and its
// stderr ends with the line msg.
func (p *project) failed(code int, msg string, args ...string) string {
	p.t.Helper()
	c, out, errOut := p.run(args...)
	if c != code || out != "" || !strings.HasSuffix("\n"+errOut, "\n"+msg+"\n") {
		p.t.Fatalf("crew %s: got code=%d stdout=%q stderr=%q; want code=%d and stderr ending %q", strings.Join(args, " "), c, out, errOut, code, msg)
	}
	return errOut
}

func (p *devProject) head() string { return p.git("log", "-1", "--format=%s") }

func (p *devProject) originHead(branch string) string {
	return gitIn(p.t, p.origin, "log", "-1", "--format=%s", branch)
}

func (p *devProject) comments(n int) []string {
	p.t.Helper()
	cs, err := p.gh().Comments(n)
	if err != nil {
		p.t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Body)
	}
	return out
}

// #7-AC1
func TestTicketStart(t *testing.T) {
	p := newDevProject(t)
	before := len(p.writes())
	out := p.start(1, "--tier", "2")
	if want := "#1 stage=4:in-progress tier=2 branch=feat/1/login-with-email worktree=" + worktree + "\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"priority:medium", "kind:feat", "tier:2", "stage:4:in-progress"}) {
		t.Fatalf("labels %q", got)
	}
	wt := filepath.Join(p.main, worktree)
	if got := gitIn(t, wt, "symbolic-ref", "--short", "HEAD"); got != "feat/1/login-with-email" {
		t.Fatalf("worktree on %q", got)
	}
	if got, want := gitIn(t, wt, "rev-parse", "HEAD"), p.git("rev-parse", "origin/main"); got != want {
		t.Fatalf("branch at %s; want origin/main %s", got, want)
	}
	// The PR opens with the first commit, and the compose project with the
	// first crew env up or crew run.
	for _, w := range p.writes()[before:] {
		if strings.Contains(w, "/pulls") {
			t.Fatalf("opened a PR: %q", w)
		}
	}
	if len(p.box.calls) != 0 {
		t.Fatalf("ran compose: %q", p.box.calls)
	}
}

// #7-AC1
func TestTicketStartJSON(t *testing.T) {
	p := newDevProject(t)
	out := p.start(1, "--tier", "1", "--json")
	if want := `{"number":1,"stage":"4:in-progress","tier":1,"branch":"feat/1/login-with-email","worktree":"` + worktree + `","compose":"crew-1"}` + "\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
}

// #7-AC1: a start that stopped half way can simply run again.
func TestTicketStartReusesTheWorktree(t *testing.T) {
	p := newDevProject(t)
	p.git("worktree", "add", "-q", "-b", "feat/1/login-with-email", filepath.Join(p.main, worktree), "origin/main")
	p.start(1, "--tier", "2")
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:4:in-progress") {
		t.Fatalf("labels %q", got)
	}
}

// #7-AC1: a start whose tier write fails leaves the ticket where start can
// run again.
func TestTicketStartTierFailure(t *testing.T) {
	p := newDevProject(t)
	failed := false
	p.fake.Hook = func(w http.ResponseWriter, r *http.Request) bool {
		if failed || r.Method != http.MethodPost || r.URL.Path != "/repos/"+p.fullName+"/issues/1/labels" {
			return false
		}
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if strings.Contains(string(body), "tier:") {
			failed = true
			w.WriteHeader(http.StatusBadGateway)
			return true
		}
		return false
	}
	p.failed(1, "unexpected github response: POST repos/user/app/issues/1/labels: HTTP 502", "ticket", "start", "1", "--tier", "2", "--lead", p.token)
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:3:eligible") {
		t.Fatalf("labels %q", got)
	}
	p.start(1, "--tier", "2")
}

// #7-AC1
func TestTicketStartRefusals(t *testing.T) {
	p := newDevProject(t)
	before := len(p.writes())
	p.refused(1, otherLead, "ticket", "start", "1", "--tier", "2")
	p.refused(2, "missing flag: --tier", "ticket", "start", "1", "--lead", p.token)
	p.refused(2, "invalid --tier: 4; want 1, 2 or 3", "ticket", "start", "1", "--tier", "4", "--lead", p.token)
	p.refused(2, "invalid --tier: x; want 1, 2 or 3", "ticket", "start", "1", "--tier", "x", "--lead", p.token)
	p.ok("ticket", "new", "Other")
	p.refused(1, "stage-changed: #2 is at 2:drafting, not 3:eligible", "ticket", "start", "2", "--tier", "2", "--lead", p.token)
	if w := p.writes()[before:]; slices.ContainsFunc(w, func(r string) bool { return !strings.HasSuffix(r, "/issues") }) {
		t.Fatalf("a refused start wrote: %q", w)
	}
	if _, err := os.Stat(filepath.Join(p.main, ".crew/worktrees")); err == nil {
		t.Fatal("a refused start made a worktree")
	}
}

func TestSlug(t *testing.T) {
	p := newDevProject(t)
	for n, c := range []struct{ title, branch string }{
		{"Fix: crash on ÜTF-8 input!!", "feat/2/fix-crash-on-tf-8-input"},
		{"A very long title that goes on and on past forty characters", "feat/3/a-very-long-title-that-goes-on-and-on"},
		{"???", "feat/4/ticket"},
	} {
		p.ok("ticket", "new", c.title)
		p.setBody(n+2, fmt.Sprintf("## Goal\nx\n## Acceptance criteria\n- [#%d-AC1] x\n", n+2))
		p.ok("ticket", "ready", fmt.Sprint(n+2))
		if out := p.start(n+2, "--tier", "1"); !strings.Contains(out, " branch="+c.branch+" ") {
			t.Errorf("%q: got %q; want branch %s", c.title, out, c.branch)
		}
	}
}

// #7-AC2
func TestTicketStartAtTheCap(t *testing.T) {
	p := newDevProject(t)
	for _, stage := range []string{"stage:4:in-progress", "stage:4:waiting-review", "stage:4:review-rejected", "stage:5:review-approved", "stage:3:eligible"} {
		if _, err := p.gh().CreateIssue(github.NewIssue{Title: "t", Labels: []string{stage, "kind:feat", "priority:medium"}}); err != nil {
			t.Fatal(err)
		}
	}
	// crew.toml allows 3 Devs; #2, #3 and #4 hold the slots.
	p.refused(1, "at-cap: 3 of 3 slots busy: #2 #3 #4", "ticket", "start", "1", "--tier", "2", "--lead", p.token)
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:3:eligible") {
		t.Fatalf("labels %q", got)
	}
	// A critical ticket gets one extra slot, and only one.
	p.gh().RemoveLabel(1, "priority:medium")
	p.gh().AddLabels(1, "priority:critical")
	p.start(1, "--tier", "2")
	p.gh().RemoveLabel(6, "priority:medium")
	p.gh().AddLabels(6, "priority:critical")
	p.refused(1, "at-cap: 4 of 4 slots busy: #1 #2 #3 #4", "ticket", "start", "6", "--tier", "2", "--lead", p.token)
}

// #7-AC3
func TestRedCommitChangesOnlyTests(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	head := p.git("rev-parse", "HEAD")
	p.write("src/app/login.py", "x = 1\n")
	p.write("tests/test_login.py", "# #1-AC1\n")
	p.box.code = 1
	p.refused(1, "red: the commit changes files that are not tests: src/app/login.py", "commit", "--red", "login rejects an unknown email")
	if got := p.git("rev-parse", "HEAD"); got != head {
		t.Fatal("a refused red step committed")
	}
	if got := p.git("diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("a refused red step left staged changes: %q", got)
	}
	if len(p.box.calls) != 0 {
		t.Fatalf("ran tests: %q", p.box.calls)
	}
	os.Remove(filepath.Join(p.dir, "src/app/login.py"))
	p.write("test_root.py", "")
	p.refused(1, "red: the commit changes files that are not tests: test_root.py", "commit", "--red", "x")
	os.Remove(filepath.Join(p.dir, "test_root.py"))
	p.git("add", "tests/test_login.py")
	p.git("commit", "-q", "-m", "test")
	os.Remove(filepath.Join(p.dir, "tests/test_login.py"))
	p.refused(1, "red: no changed test file to run", "commit", "--red", "x")
}

// #7-AC3
func TestRedCommitNeedsFailingTests(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.write("tests/test_login.py", "# #1-AC1\n")
	p.write("tests/unit/test_link.py", "# #1-AC1\n")
	p.box.code = 0
	p.refused(1, "red: the tests pass; a red step needs failing tests", "commit", "--red", "login rejects an unknown email")
	want := p.dir + ` -p crew-1 run --rm -T app sh -c pytest -q "$@" sh tests/test_login.py tests/unit/test_link.py`
	if !slices.Equal(p.box.calls, []string{want}) {
		t.Fatalf("calls %q; want %q", p.box.calls, want)
	}
	// An exit the engine or the shell uses is not a test result.
	for _, code := range []int{125, 126, 127} {
		p.box.out, p.box.code = "Error: no such service: app\n", code
		errOut := p.failed(1, fmt.Sprintf("red: the tests could not run: exit %d", code), "commit", "--red", "x")
		if !strings.HasPrefix(errOut, "Error: no such service: app\n") {
			t.Fatalf("stderr %q", errOut)
		}
	}
	if got := p.comments(1); len(got) != 0 {
		t.Fatalf("logged a red result: %q", got)
	}
	p.box.out, p.box.code = "", 1
	p.ok("commit", "--red", "login rejects an unknown email")
	if got := p.head(); got != "[feat:#1] red: login rejects an unknown email" {
		t.Fatalf("commit %q", got)
	}
}

// #7-AC4
func TestRedResultIsLogged(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.write("tests/test_login.py", "def test_unknown():  # #1-AC1\n    pass\n\n# #1-AC2 and #1-AC1 again\n")
	var out []string
	for k := 1; k <= 25; k++ {
		out = append(out, fmt.Sprintf("line %d", k))
	}
	p.box.out, p.box.code = strings.Join(out, "\n")+"\n", 1
	p.ok("commit", "--red", "login rejects an unknown email")
	want := "<!-- crew:log dev -->\nred: #1-AC1 #1-AC2\ntests: tests/test_login.py\n```\n" + strings.Join(out[5:], "\n") + "\n```\n"
	if got := p.comments(1); !slices.Equal(got, []string{want}) {
		t.Fatalf("comments %q; want %q", got, []string{want})
	}
	// Keyed by test, not by commit, so a rebase leaves it valid.
	sha := p.git("rev-parse", "HEAD")
	if strings.Contains(p.comments(1)[0], sha[:7]) {
		t.Fatal("the red record names the commit")
	}
	if got := p.ok("ticket", "show", "1"); !strings.Contains(got, " log: red: #1-AC1 #1-AC2\n") {
		t.Fatalf("timeline %q", got)
	}
}

// #7-AC5
func TestGreenCommitNeedsPassingTests(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.write("tests/test_login.py", "# #1-AC1\n")
	p.box.code = 1
	p.ok("commit", "--red", "login rejects an unknown email")
	p.box.calls = nil
	p.write("src/app/login.py", "x = 1\n")
	p.box.out = "FAILED tests/test_login.py::test_unknown\n"
	errOut := p.failed(1, "green: the tests fail", "commit", "--green", "reject unknown emails")
	if !strings.Contains(errOut, "FAILED tests/test_login.py::test_unknown\n") {
		t.Fatalf("stderr %q has no test output", errOut)
	}
	if got := p.head(); got != "[feat:#1] red: login rejects an unknown email" {
		t.Fatalf("a refused green step committed: %q", got)
	}
	// Green runs every test file the branch has changed since main.
	want := p.dir + ` -p crew-1 run --rm -T app sh -c pytest -q "$@" sh tests/test_login.py`
	if !slices.Equal(p.box.calls, []string{want}) {
		t.Fatalf("calls %q; want %q", p.box.calls, want)
	}
	p.box.code = 0
	p.ok("commit", "--green", "reject unknown emails")
	if got := p.head(); got != "[feat:#1] green: reject unknown emails" {
		t.Fatalf("commit %q", got)
	}
}

// #7-AC5
func TestRefactorCommit(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.write("tests/test_login.py", "# #1-AC1\n")
	p.box.code = 1
	p.ok("commit", "--red", "login rejects an unknown email")
	p.box.code = 0

	p.write("tests/test_more.py", "x\n")
	p.refused(1, "refactor: the commit adds a test file: tests/test_more.py", "commit", "--refactor", "tidy")
	os.Remove(filepath.Join(p.dir, "tests/test_more.py"))

	p.write("tests/test_login.py", "# #1-AC1\n# #1-AC2\n")
	p.refused(1, "refactor: the commit adds tests for #1-AC2", "commit", "--refactor", "tidy")

	// Moving a test is fine.
	p.write("tests/test_login.py", "x = 1\n# #1-AC1 moved\n")
	p.box.code = 1
	p.failed(1, "refactor: the tests fail", "commit", "--refactor", "tidy")
	p.box.code = 0
	p.ok("commit", "--refactor", "tidy")
	if got := p.head(); got != "[feat:#1] refactor: tidy" {
		t.Fatalf("commit %q", got)
	}
}

// #7-AC6
func TestCommitPrefixPushAndPR(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.write("tests/test_login.py", "# #1-AC1\n")
	p.box.code = 1
	out := p.ok("commit", "--red", "login rejects an unknown email")
	sha := p.git("rev-parse", "--short", "HEAD")
	if want := sha + " [feat:#1] red: login rejects an unknown email\npushed feat/1/login-with-email\nopened PR #2\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	if got := p.originHead("feat/1/login-with-email"); got != "[feat:#1] red: login rejects an unknown email" {
		t.Fatalf("origin has %q", got)
	}
	pr, err := p.gh().PullRequest(2)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Title != "[feat:#1] Login with email" || pr.Body != "Ticket #1" || pr.Draft || pr.Head.Ref != "feat/1/login-with-email" || pr.Base.Ref != "main" {
		t.Fatalf("PR %+v", pr)
	}

	p.write("README.md", "docs\n")
	out = p.ok("commit", "document login")
	sha = p.git("rev-parse", "--short", "HEAD")
	if want := sha + " [feat:#1] document login\npushed feat/1/login-with-email\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	if got := p.originHead("feat/1/login-with-email"); got != "[feat:#1] document login" {
		t.Fatalf("origin has %q", got)
	}
	if len(p.box.calls) != 1 {
		t.Fatalf("a plain commit ran tests: %q", p.box.calls)
	}

	p.write("README.md", "more docs\n")
	out = p.ok("commit", "--json", "more docs")
	sha = p.git("rev-parse", "HEAD")
	if want := `{"commit":"` + sha + `","message":"[feat:#1] more docs","branch":"feat/1/login-with-email","pr":2,"opened":false}` + "\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
}

// #7-AC6
func TestCommitRefusals(t *testing.T) {
	p := newDevProject(t)
	p.refused(1, "not on a ticket branch: main", "commit", "x")
	p.inWorktree()
	p.refused(1, "nothing to commit", "commit", "x")
	p.refused(2, "missing argument: <message>", "commit", "--green")
	p.refused(2, "only one of --red, --green and --refactor", "commit", "--red", "--green", "x")
	p.refused(2, "unknown flag: --blue", "commit", "--blue", "x")
}

// #7-AC7
func TestEnv(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	for _, c := range []struct{ cmd, out, args string }{
		{"up", "crew-1 up\n", "-p crew-1 up -d"},
		{"down", "crew-1 down\n", "-p crew-1 down --volumes"},
	} {
		p.box.calls = nil
		if out := p.ok("env", c.cmd); out != c.out {
			t.Fatalf("env %s: got %q", c.cmd, out)
		}
		if want := p.dir + " " + c.args; !slices.Equal(p.box.calls, []string{want}) {
			t.Fatalf("calls %q; want %q", p.box.calls, want)
		}
	}
	p.box.calls, p.box.out = nil, "app  | started\n"
	code, out, errOut := p.run("env", "logs")
	if code != 0 || out != "app  | started\n" || errOut != "" {
		t.Fatalf("env logs: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if want := p.dir + " -p crew-1 logs --no-color"; !slices.Equal(p.box.calls, []string{want}) {
		t.Fatalf("calls %q; want %q", p.box.calls, want)
	}
	p.box.out, p.box.code = "Error: no such image\n", 125
	errOut = p.failed(1, "env up: exit 125", "env", "up")
	if !strings.HasPrefix(errOut, "Error: no such image\n") {
		t.Fatalf("stderr %q", errOut)
	}
	p.refused(2, "usage: crew env up|down|logs", "env")
	p.refused(2, "unknown command: env bogus", "env", "bogus")
}

// #7-AC7
func TestRun(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.box.out, p.box.code = "3 passed\n", 0
	code, out, errOut := p.run("run", "test-changed", "-k", "login", "tests/test_login.py")
	if code != 0 || out != "3 passed\n" || errOut != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if want := p.dir + ` -p crew-1 run --rm -T app sh -c pytest -q "$@" sh -k login tests/test_login.py`; !slices.Equal(p.box.calls, []string{want}) {
		t.Fatalf("calls %q; want %q", p.box.calls, want)
	}
	// The command's exit code is crew's.
	p.box.out, p.box.code = "1 failed\n", 1
	if code, out, _ := p.run("run", "test"); code != 1 || out != "1 failed\n" {
		t.Fatalf("got code=%d stdout=%q", code, out)
	}
	p.refused(2, "missing argument: <command>", "run")
	p.refused(2, "unknown profile command: deploy", "run", "deploy")
	p.refused(1, "the python profile has no e2e command", "run", "e2e")
}

// #7-AC8
func TestAsk(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	before := p.fake.IssueLabels(1)
	if out := p.ok("ticket", "ask", "Error page or redirect?"); out != "#1 question posted\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.comments(1); !slices.Equal(got, []string{"<!-- crew:question dev -->\nError page or redirect?"}) {
		t.Fatalf("comments %q", got)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, before) {
		t.Fatalf("labels %q; want %q", got, before)
	}
	p.refused(2, "missing argument: <question>", "ticket", "ask")
}

// #7-AC8
func TestStuck(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	if out := p.ok("ticket", "stuck", "The session table has no email column."); out != "#1 stage=4:review-rejected\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"priority:medium", "kind:feat", "tier:2", "stage:4:review-rejected"}) {
		t.Fatalf("labels %q", got)
	}
	if got := p.comments(1); !slices.Equal(got, []string{"<!-- crew:log dev -->\nstuck: The session table has no email column."}) {
		t.Fatalf("comments %q", got)
	}
	p.refused(1, "stage-changed: #1 is at 4:review-rejected, not 4:in-progress", "ticket", "stuck", "again")
	if got := len(p.comments(1)); got != 1 {
		t.Fatalf("a refused stuck posted; %d comments", got)
	}
}

// Dev commands run only in a ticket's worktree.
func TestDevCommandsNeedATicketBranch(t *testing.T) {
	p := newDevProject(t)
	for _, args := range [][]string{{"env", "up"}, {"run", "test"}, {"ticket", "ask", "q"}, {"ticket", "stuck", "r"}} {
		p.refused(1, "not on a ticket branch: main", args...)
	}
}
