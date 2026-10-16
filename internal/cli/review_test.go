package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// waiting makes ticket 1 wait for review: its gated commit is pushed and
// its PR is #2. It returns the gated commit.
func (p *devProject) waiting() string {
	p.t.Helper()
	sha := p.submittable()
	p.ok("ticket", "submit")
	p.box.calls = nil
	return sha
}

// verdict checks that PR 2 has one review, a comment review of commit sha
// with body, and that ticket 1's last comment is body too.
func (p *devProject) verdict(sha, body string) {
	p.t.Helper()
	rs, err := p.gh().Reviews(2)
	if err != nil {
		p.t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].State != "COMMENTED" || rs[0].CommitID != sha || rs[0].Body != body {
		p.t.Fatalf("reviews %+v; want one comment review of %s with %q", rs, sha, body)
	}
	if cs := p.comments(1); cs[len(cs)-1] != body {
		p.t.Fatalf("last ticket comment %q; want %q", cs[len(cs)-1], body)
	}
}

// #9-AC1
func TestReviewApproves(t *testing.T) {
	p := newDevProject(t)
	sha := p.waiting()
	p.dir = p.main
	if out := p.ok("review", "1", "--approve"); out != "#1 stage=5:review-approved\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:5:review-approved") || slices.Contains(got, "stage:4:waiting-review") {
		t.Fatalf("labels %q", got)
	}
	p.verdict(sha, "<!-- crew:review reviewer -->\napproved")
}

// #9-AC2
func TestReviewRejects(t *testing.T) {
	p := newDevProject(t)
	sha := p.waiting()
	p.dir = p.main
	if out := p.ok("review", "--reject", "the link never expires", "1"); out != "#1 stage=4:review-rejected\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:4:review-rejected") || slices.Contains(got, "stage:4:waiting-review") {
		t.Fatalf("labels %q", got)
	}
	p.verdict(sha, "<!-- crew:review reviewer -->\nrejected: the link never expires")
	// crew ticket show gives the DM the reason.
	if out := p.ok("ticket", "show", "1"); !strings.Contains(out, "[review] REVIEWER 2026-01-01T00:00Z: rejected: the link never expires\n") {
		t.Fatalf("show:\n%s", out)
	}
}

// #9-AC1 #9-AC2
func TestReviewJSON(t *testing.T) {
	p := newDevProject(t)
	p.waiting()
	if out := p.ok("review", "1", "--approve", "--json"); out != `{"number":1,"pr":2,"stage":"5:review-approved"}`+"\n" {
		t.Fatalf("got %q", out)
	}
}

// #9-AC1 #9-AC2
func TestReviewRefusals(t *testing.T) {
	p := newDevProject(t)
	p.submittable()
	before := len(p.writes())
	p.refused(2, "missing flag: --approve or --reject", "review", "1")
	p.refused(2, "only one of --approve and --reject", "review", "1", "--approve", "--reject", "no")
	p.refused(2, "missing reason: --reject", "review", "1", "--reject", " ")
	p.refused(2, "missing argument: <n>", "review", "--approve")
	p.refused(1, "stage-changed: #1 is at 4:in-progress, not 4:waiting-review", "review", "1", "--approve")
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("a refused review wrote: %q", w)
	}
}

// mirror makes the fake GitHub follow every push to origin, as GitHub does.
func (p *devProject) mirror() {
	p.fake.Hook = func(http.ResponseWriter, *http.Request) bool {
		refs := gitIn(p.t, p.origin, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
		for l := range strings.SplitSeq(refs, "\n") {
			name, sha, _ := strings.Cut(l, " ")
			p.fake.SetBranch(name, sha)
		}
		return false
	}
}

// approved makes ticket 1 approved, with the lead DM in the main checkout,
// and returns the gated commit.
func (p *devProject) approved() string {
	p.t.Helper()
	sha := p.waiting()
	p.dir = p.main
	p.ok("review", "1", "--approve")
	p.mirror()
	return sha
}

// pushMain commits a file to main and pushes it, as another merge would.
func (p *devProject) pushMain(name, body string) string {
	p.t.Helper()
	p.writeMain(name, body)
	gitIn(p.t, p.main, "add", name)
	gitIn(p.t, p.main, "commit", "-q", "-m", "other work")
	gitIn(p.t, p.main, "push", "-q", "origin", "main")
	return gitIn(p.t, p.main, "rev-parse", "HEAD")
}

func (p *devProject) merge(args ...string) string {
	p.t.Helper()
	return p.ok(append([]string{"merge", "1", "--lead", p.token}, args...)...)
}

func (p *devProject) stage(want string) {
	p.t.Helper()
	var stages []string
	for _, l := range p.fake.IssueLabels(1) {
		if strings.HasPrefix(l, "stage:") {
			stages = append(stages, l)
		}
	}
	if !slices.Equal(stages, []string{"stage:" + want}) {
		p.t.Fatalf("stages %q; want %s", stages, want)
	}
}

// merged checks that origin's main is at sha, with no merge commit, and
// that PR 2 is merged.
func (p *devProject) merged(sha string) {
	p.t.Helper()
	if got := gitIn(p.t, p.origin, "rev-parse", "main"); got != sha {
		p.t.Fatalf("origin main at %s; want %s", got, sha)
	}
	if got := gitIn(p.t, p.origin, "rev-list", "--merges", "main"); got != "" {
		p.t.Fatalf("merge commits on main: %s", got)
	}
	if got := gitIn(p.t, p.main, "rev-parse", "main"); got != sha {
		p.t.Fatalf("local main at %s; want %s", got, sha)
	}
	pr, err := p.gh().PullRequest(2)
	if err != nil || !pr.Merged {
		p.t.Fatalf("PR %+v %v; want merged", pr, err)
	}
}

// cleaned checks that the ticket's worktree is gone and its compose project
// was taken down.
func (p *devProject) cleaned() {
	p.t.Helper()
	wt := filepath.Join(p.main, worktree)
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		p.t.Fatalf("worktree left behind: %v", err)
	}
	if got := gitIn(p.t, p.main, "worktree", "list"); strings.Contains(got, ".crew/worktrees") {
		p.t.Fatalf("worktrees still registered: %s", got)
	}
	if last := p.box.calls[len(p.box.calls)-1]; last != wt+" -p crew-1 down --volumes --remove-orphans" {
		p.t.Fatalf("last compose call %q", last)
	}
}

const removed = "removed " + worktree + " crew-1\n"

// #9-AC3 #9-AC4 #9-AC6 #9-AC7
func TestMerge(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	statuses := len(p.statusWrites())
	out := p.merge()
	if want := "merged " + sha[:7] + " into main\n#1 stage=9:done\n" + removed; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	p.merged(sha)
	p.stage("9:done")
	if i := p.issue(1); i.State != "closed" || i.StateReason != "completed" {
		t.Fatalf("issue %s %s", i.State, i.StateReason)
	}
	// The commit already passed the gate, so it does not run again.
	if got := len(p.statusWrites()); got != statuses {
		t.Fatalf("gate ran again: %q", p.statusWrites())
	}
	if len(p.box.calls) != 1 {
		t.Fatalf("compose calls %q", p.box.calls)
	}
	p.cleaned()
	// The timeline shows every stage it went through.
	show := p.ok("ticket", "show", "1")
	for _, s := range []string{"pending-merge", "merged", "done"} {
		if !strings.Contains(show, "2026-01-01T00:00Z "+s) {
			t.Fatalf("no %s in:\n%s", s, show)
		}
	}
}

// #9-AC3 #9-AC4
func TestMergeAfterMainMoved(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	other := p.pushMain("docs/notes.md", "notes\n")
	out := p.merge()
	rebased := gitIn(t, p.origin, "rev-parse", branch)
	if rebased == sha {
		t.Fatal("the branch was not rebased")
	}
	if want := "rebased " + branch + " onto main: " + rebased[:7] + "\ngate passed " + rebased[:7] + "\nmerged " + rebased[:7] + " into main\n#1 stage=9:done\n" + removed; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	p.merged(rebased)
	if got := gitIn(t, p.origin, "rev-parse", rebased+"~2"); got != other {
		t.Fatalf("rebased onto %s; want %s", got, other)
	}
	if got := p.gateStatus(rebased); len(got) != 1 || got[0].State != "success" {
		t.Fatalf("statuses %+v", got)
	}
	gate := filepath.Join(p.main, gateDir)
	if want := gateCalls(gate, pythonSteps...); !slices.Equal(p.box.calls[:len(want)], want) {
		t.Fatalf("calls:\n%s\nwant first:\n%s", strings.Join(p.box.calls, "\n"), strings.Join(want, "\n"))
	}
	if _, err := os.Stat(filepath.Join(p.main, ".crew/worktrees/merge/1")); err == nil {
		t.Fatal("merge worktree left behind")
	}
	p.stage("9:done")
	p.cleaned()
}

// #9-AC3: a rebased commit that fails the gate is not merged.
func TestMergeGateFails(t *testing.T) {
	p := newDevProject(t)
	p.approved()
	other := p.pushMain("docs/notes.md", "notes\n")
	p.box.answer = func(_ string, args []string) (string, int) {
		if slices.Contains(args, `pytest -q "$@"`) {
			return "1 failed\n", 1
		}
		return "", 0
	}
	code, out, errOut := p.run("merge", "1", "--lead", p.token)
	rebased := gitIn(t, p.origin, "rev-parse", branch)
	if want := "rebased " + branch + " onto main: " + rebased[:7] + "\n"; code != 1 || out != want || !strings.HasSuffix(errOut, "\ngate failed: test: exit 1; full output: "+gateLog+"\n") {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if got := gitIn(t, p.origin, "rev-parse", "main"); got != other {
		t.Fatalf("origin main at %s; want %s", got, other)
	}
	p.stage("6:pending-merge")
	if _, err := os.Stat(filepath.Join(p.main, worktree)); err != nil {
		t.Fatalf("worktree removed: %v", err)
	}
}

// #9-AC5
func TestMergeConflict(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	before := gitIn(t, p.main, "rev-parse", "HEAD")
	other := p.pushMain("src/app/login.py", "def login(): return False\n")
	statuses := len(p.statusWrites())
	p.failed(1, "merge-conflict: #1 stays at 6:pending-merge: src/app/login.py", "merge", "1", "--lead", p.token)
	p.stage("6:pending-merge")
	if got := gitIn(t, p.origin, "rev-parse", "main"); got != other {
		t.Fatalf("origin main at %s; want %s", got, other)
	}
	if got := gitIn(t, p.origin, "rev-parse", branch); got != sha {
		t.Fatalf("branch at %s; want %s", got, sha)
	}
	if len(p.box.calls) != 0 || len(p.statusWrites()) != statuses {
		t.Fatalf("ran something: %q %q", p.box.calls, p.statusWrites())
	}
	if got := gitIn(t, p.main, "worktree", "list"); strings.Contains(got, "merge") {
		t.Fatalf("merge worktree left: %s", got)
	}
	if _, err := os.Stat(filepath.Join(p.main, worktree)); err != nil {
		t.Fatalf("worktree removed: %v", err)
	}
	// Once the DM has handled it, merge runs again from 6:pending-merge.
	gitIn(t, p.main, "push", "-q", "-f", "origin", before+":refs/heads/main")
	gitIn(t, p.main, "reset", "-q", "--hard", before)
	if out := p.merge(); !strings.HasPrefix(out, "merged "+sha[:7]+" into main\n") {
		t.Fatalf("got %q", out)
	}
	p.merged(sha)
	p.stage("9:done")
}

// #9-AC6: with a deployment workflow, the ticket waits at 7:merged.
func TestMergeWithDeploymentWorkflow(t *testing.T) {
	p := newDevProject(t)
	p.pushMain(".github/workflows/deploy.yml", "on: push\n")
	sha := p.approved()
	if out := p.merge(); out != "merged "+sha[:7]+" into main\n#1 stage=7:merged\n"+removed {
		t.Fatalf("got %q", out)
	}
	p.stage("7:merged")
	if i := p.issue(1); i.State != "open" {
		t.Fatalf("issue %s", i.State)
	}
}

// #9-AC3
func TestMergeJSON(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	if out := p.merge("--json"); out != `{"number":1,"commit":"`+sha+`","rebased":false,"flags":[],"stage":"9:done"}`+"\n" {
		t.Fatalf("got %q", out)
	}
}

// #9-AC5: a merge that stops prints no text lines in place of JSON.
func TestMergeJSONStops(t *testing.T) {
	p := newDevProject(t)
	p.approved()
	p.pushMain("docs/notes.md", "notes\n")
	p.box.code = 1
	p.failed(1, "gate failed: fresh: exit 1; full output: "+gateLog, "merge", "1", "--lead", p.token, "--json")
}

// #9-AC3
func TestMergeRefusals(t *testing.T) {
	p := newDevProject(t)
	p.waiting()
	p.dir = p.main
	before := len(p.writes())
	p.refused(1, otherLead, "merge", "1")
	p.refused(2, "missing argument: <n>", "merge", "--lead", p.token)
	p.refused(1, "stage-changed: #1 is at 4:waiting-review, not 5:review-approved", "merge", "1", "--lead", p.token)
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("a refused merge wrote: %q", w)
	}
	// A ticket without a single kind has no branch to merge and keeps its stage.
	p.ok("review", "1", "--approve")
	p.fake.SetIssueLabels(1, "stage:5:review-approved", "kind:feat", "kind:fix")
	p.refused(1, "#1 has no single kind: kind=feat,fix", "merge", "1", "--lead", p.token)
	p.stage("5:review-approved")
}
