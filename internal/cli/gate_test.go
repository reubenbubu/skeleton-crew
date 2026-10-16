package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

const (
	gateDir = ".crew/worktrees/gate/1"
	gateLog = ".crew/logs/20260101T000000Z-gate-1.log"
	branch  = "feat/1/login-with-email"
)

// submittable makes ticket 1 ready to submit: a red and a green commit for
// #1-AC1, pushed. It returns the pushed commit.
func (p *devProject) submittable() string {
	p.t.Helper()
	p.inWorktree()
	p.write("tests/test_login.py", "def test_known():  # #1-AC1\n    assert login()\n")
	p.box.code = 1
	p.ok("commit", "--red", "a known email gets a link")
	p.write("src/app/login.py", "def login(): return True\n")
	p.box.code = 0
	p.ok("commit", "--green", "send the link")
	return p.pushed()
}

// pushed tells the fake GitHub about the branch's pushed commit and returns
// it.
func (p *devProject) pushed() string {
	p.t.Helper()
	sha := p.git("rev-parse", "HEAD")
	p.fake.SetBranch(branch, sha)
	p.box.calls = nil
	return sha
}

func (p *devProject) gateStatus(sha string) []github.Status {
	p.t.Helper()
	s, err := p.gh().CombinedStatus(sha)
	if err != nil {
		p.t.Fatal(err)
	}
	return s.Statuses
}

func (p *devProject) statusWrites() []string {
	var out []string
	for _, w := range p.writes() {
		if strings.Contains(w, "/statuses/") {
			out = append(out, w)
		}
	}
	return out
}

func gateCalls(dir string, steps ...string) []string {
	out := []string{dir + " -p crew-1-gate down --volumes --remove-orphans", dir + " -p crew-1-gate build"}
	for _, s := range steps {
		out = append(out, dir+` -p crew-1-gate run --rm -T app sh -c `+s+` "$@" sh`)
	}
	return append(out, dir+" -p crew-1-gate down --volumes --remove-orphans")
}

var pythonSteps = []string{"python -m compileall -q src", "ruff check . && ruff format --check .", "pytest -q"}

// #8-AC1 #8-AC2 #8-AC3 #8-AC6
func TestSubmitPasses(t *testing.T) {
	p := newDevProject(t)
	sha := p.submittable()
	dir := filepath.Join(p.main, gateDir)
	// The gate runs on the exact pushed commit, in a clean checkout.
	p.box.answer = func(d string, args []string) (string, int) {
		if got := gitIn(t, d, "rev-parse", "HEAD"); got != sha {
			t.Errorf("gate checkout at %s; want %s", got, sha)
		}
		if got := gitIn(t, d, "status", "--porcelain"); got != "" {
			t.Errorf("gate checkout not clean: %q", got)
		}
		return "ok\n", 0
	}
	out := p.ok("ticket", "submit")
	if want := "gate passed " + sha[:7] + "\n#1 stage=4:waiting-review\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	if want := gateCalls(dir, pythonSteps...); !slices.Equal(p.box.calls, want) {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(p.box.calls, "\n"), strings.Join(want, "\n"))
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:4:waiting-review") || slices.Contains(got, "stage:4:in-progress") {
		t.Fatalf("labels %q", got)
	}
	if got := p.gateStatus(sha); len(got) != 1 || got[0].Context != "crew/gate" || got[0].State != "success" || got[0].Description != "gate passed" {
		t.Fatalf("statuses %+v", got)
	}
	// pending first, then the result.
	if got := p.statusWrites(); len(got) != 2 {
		t.Fatalf("status writes %q", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("gate worktree left behind: %v", err)
	}
	if got := gitIn(t, p.main, "worktree", "list"); strings.Contains(got, gateDir) {
		t.Fatalf("gate worktree still registered: %s", got)
	}
	log, err := os.ReadFile(filepath.Join(p.main, gateLog))
	if err != nil || !strings.Contains(string(log), "== test: run --rm -T app sh -c pytest -q \"$@\" sh\nok\n== test: exit 0\n") {
		t.Fatalf("log %q %v", log, err)
	}
}

// #8-AC2 #8-AC3
func TestSubmitFails(t *testing.T) {
	p := newDevProject(t)
	sha := p.submittable()
	var lines []string
	for k := 1; k <= 25; k++ {
		lines = append(lines, fmt.Sprintf("line %d", k))
	}
	p.box.answer = func(_ string, args []string) (string, int) {
		if slices.Contains(args, `pytest -q "$@"`) {
			return strings.Join(lines, "\n") + "\n", 1
		}
		return "", 0
	}
	errOut := p.failed(1, "gate failed: test: exit 1; full output: "+gateLog, "ticket", "submit")
	if want := strings.Join(lines[5:], "\n") + "\n"; !strings.HasPrefix(errOut, want) {
		t.Fatalf("stderr %q", errOut)
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:4:in-progress") || slices.Contains(got, "stage:4:waiting-review") {
		t.Fatalf("labels %q", got)
	}
	if got := p.gateStatus(sha); len(got) != 1 || got[0].State != "failure" || got[0].Description != "gate failed: test: exit 1" {
		t.Fatalf("statuses %+v", got)
	}
	if log := p.readMain(gateLog); !strings.Contains(log, "line 1\n") || !strings.Contains(log, "== test: exit 1\n") {
		t.Fatalf("log %q", log)
	}
	if _, err := os.Stat(filepath.Join(p.main, gateDir)); !os.IsNotExist(err) {
		t.Fatalf("gate worktree left behind: %v", err)
	}
}

func (p *devProject) readMain(name string) string {
	p.t.Helper()
	b, err := os.ReadFile(filepath.Join(p.main, name))
	if err != nil {
		p.t.Fatal(err)
	}
	return string(b)
}

// #8-AC1
func TestSubmitRefusals(t *testing.T) {
	p := newDevProject(t)
	p.refused(1, "not on a ticket branch: main", "ticket", "submit")
	p.inWorktree()
	p.refused(1, "no commits on "+branch, "ticket", "submit")
	p.write("tests/test_login.py", "# #1-AC1\n")
	p.refused(1, "uncommitted changes; crew commit them first", "ticket", "submit")
	p.git("add", "tests/test_login.py")
	p.git("commit", "-q", "-m", "not pushed")
	p.refused(1, "the last commit is not pushed to origin/"+branch, "ticket", "submit")
	p.git("push", "-q", "origin", branch)
	p.git("commit", "-q", "--allow-empty", "-m", "not pushed either")
	p.refused(1, "the last commit is not pushed to origin/"+branch, "ticket", "submit")
	p.git("push", "-q", "origin", branch)
	p.pushed()
	p.fake.SetIssueLabels(1, "priority:medium", "kind:feat", "tier:2", "stage:4:waiting-review")
	p.refused(1, "stage-changed: #1 is at 4:waiting-review, not 4:in-progress", "ticket", "submit")
	p.refused(2, "unexpected argument: x", "ticket", "submit", "x")
	if len(p.box.calls) != 0 || len(p.statusWrites()) != 0 {
		t.Fatalf("a refused submit ran the gate: %q %q", p.box.calls, p.statusWrites())
	}
}

// #8-AC4
func TestSubmitNeedsAFailingTestPerCriterion(t *testing.T) {
	p := newDevProject(t)
	p.setBody(1, "## Goal\nLog in.\n## Acceptance criteria\n- [#1-AC1] A known email gets a link.\n- [#1-AC2] An unknown email gets an error.\n- [#1-AC3] Links expire.\n")
	p.inWorktree()
	// #1-AC2 failed, but its test is gone; #1-AC3 has a test that never failed.
	p.write("tests/test_login.py", "# #1-AC1\n# #1-AC2\n")
	p.box.code = 1
	p.ok("commit", "--red", "known and unknown emails")
	p.write("tests/test_login.py", "# #1-AC1\n# #1-AC3\n")
	p.write("src/app/login.py", "x = 1\n")
	p.box.code = 0
	p.ok("commit", "--green", "send the link")
	sha := p.pushed()
	errOut := p.failed(1, "gate failed: tdd", "ticket", "submit")
	if want := "tdd: no red log names #1-AC3\ntdd: no test names #1-AC2\ngate failed: tdd\n"; errOut != want {
		t.Fatalf("stderr %q; want %q", errOut, want)
	}
	if len(p.box.calls) != 0 {
		t.Fatalf("ran containers: %q", p.box.calls)
	}
	if got := p.gateStatus(sha); len(got) != 1 || got[0].State != "failure" || got[0].Description != "gate failed: tdd" {
		t.Fatalf("statuses %+v", got)
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:4:in-progress") {
		t.Fatalf("labels %q", got)
	}
}

// #8-AC4: a branch with only changes that have no behaviour needs no red
// test.
func TestSubmitWithoutBehaviour(t *testing.T) {
	p := newDevProject(t)
	p.inWorktree()
	p.write("README.md", "docs\n")
	p.ok("commit", "document login")
	p.pushed()
	p.ok("ticket", "submit")
}

// #8-AC5
func TestSubmitRefusesSkippedTests(t *testing.T) {
	p := newDevProject(t)
	// A skip that main already has does not block the branch.
	p.write("tests/test_old.py", "import pytest\n\n@pytest.mark.skip\ndef test_old(): pass\n")
	p.git("add", "tests/test_old.py")
	p.git("commit", "-q", "-m", "old skip")
	p.git("push", "-q", "origin", "main")
	p.fake.SetBranch("main", p.git("rev-parse", "HEAD"))
	sha := p.submittable()
	// Moving a test that main already skips adds no skip.
	os.MkdirAll(filepath.Join(p.dir, "tests/unit"), 0o755)
	p.git("mv", "tests/test_old.py", "tests/unit/test_old.py")
	p.ok("commit", "move the old test")
	p.pushed()
	p.ok("ticket", "submit")
	p.fake.SetIssueLabels(1, "priority:medium", "kind:feat", "tier:2", "stage:4:in-progress")
	p.write("tests/test_login.py", "import pytest\n\ndef test_known():  # #1-AC1\n    pytest.skip(\"later\")\n    assert login()\n")
	p.ok("commit", "--refactor", "skip for now")
	sha = p.pushed()
	errOut := p.failed(1, "gate failed: skipped", "ticket", "submit")
	if want := "skipped: tests/test_login.py:4: pytest.skip(\ngate failed: skipped\n"; errOut != want {
		t.Fatalf("stderr %q; want %q", errOut, want)
	}
	if got := p.gateStatus(sha); len(got) != 1 || got[0].Description != "gate failed: skipped" {
		t.Fatalf("statuses %+v", got)
	}
}

// #8-AC5: stale tests are flagged, and do not fail the gate.
func TestSubmitFlagsStaleTests(t *testing.T) {
	p := newDevProject(t)
	p.ok("ticket", "new", "Old")
	p.setBody(2, "## Goal\nx\n## Acceptance criteria\n- [#2-AC1] (revised 2099-01-01T00:00Z) Changed later.\n- [#2-AC2] Still the same.\n- [#2-AC3] (revised 2000-01-01T00:00Z) Changed before the test.\n")
	p.ok("ticket", "new", "Dropped")
	p.setBody(3, "## Goal\nx\n## Acceptance criteria\n- [#3-AC1] x\n")
	p.fake.SetIssueLabels(3, "priority:medium", "kind:feat", "stage:0:cancelled")
	p.write("tests/test_old.py", "# #2-AC1 #2-AC2 #2-AC3 #2-AC4\n# #3-AC1 #7-AC1\n")
	p.git("add", "tests/test_old.py")
	p.git("commit", "-q", "-m", "old tests")
	p.git("push", "-q", "origin", "main")
	p.fake.SetBranch("main", p.git("rev-parse", "HEAD"))
	sha := p.submittable()
	out := p.ok("ticket", "submit")
	want := "flag: tests/test_old.py names #2-AC1: revised 2099-01-01T00:00Z\n" +
		"flag: tests/test_old.py names #2-AC4: removed\n" +
		"flag: tests/test_old.py names #3-AC1: cancelled\n" +
		"flag: tests/test_old.py names #7-AC1: removed\n" +
		"gate passed " + sha[:7] + "\n#1 stage=4:waiting-review\n"
	if out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
}

// #8-AC1
func TestSubmitJSON(t *testing.T) {
	p := newDevProject(t)
	sha := p.submittable()
	out := p.ok("ticket", "submit", "--json")
	if want := `{"number":1,"commit":"` + sha + `","stage":"4:waiting-review","flags":[]}` + "\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
}

// #8-AC2: only the newest 50 logs are kept.
func TestSubmitPrunesLogs(t *testing.T) {
	p := newDevProject(t)
	p.submittable()
	for k := range 55 {
		p.writeMain(fmt.Sprintf(".crew/logs/20250101T0000%02dZ-gate-1.log", k), "old\n")
	}
	p.writeMain(".crew/logs/notes.txt", "mine\n")
	p.ok("ticket", "submit")
	entries, err := os.ReadDir(filepath.Join(p.main, ".crew/logs"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 51 || names[0] != "20250101T000006Z-gate-1.log" || !slices.Contains(names, "20260101T000000Z-gate-1.log") || !slices.Contains(names, "notes.txt") {
		t.Fatalf("logs %q", names)
	}
}

func (p *devProject) writeMain(name, body string) {
	p.t.Helper()
	path := filepath.Join(p.main, name)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		p.t.Fatal(err)
	}
}
