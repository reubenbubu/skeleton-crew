package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// deployed merges ticket 1, whose PR is #2, with a deployment workflow at
// name, and returns the merged commit.
func (p *devProject) deployed(name string) string {
	p.t.Helper()
	p.pushMain(".github/workflows/"+name, "on: push\n")
	sha := p.approved()
	p.merge()
	p.stage("7:merged")
	return sha
}

func (p *devProject) leadStatus(args ...string) string {
	p.t.Helper()
	return p.ok(append([]string{"status", "--as", "dm", "--lead", p.token}, args...)...)
}

const merged1 = "#1 stage=7:merged priority=medium kind=feat tier=2 · Login with email\n"
const deploying1 = "#1 stage=8:deploying priority=medium kind=feat tier=2 · Login with email\n"

// #14-AC3
func TestStatusAdvancesDeployments(t *testing.T) {
	p := newDevProject(t)
	sha := p.deployed("deploy.yml")

	// Without the lead's token, status only proposes.
	if out := p.ok("status", "--as", "dm"); !strings.HasSuffix(out, "#1 7:merged: advance from the deployment status\n") {
		t.Fatalf("got\n%s", out)
	}
	if out := p.leadStatus(); out != "=== board\n"+merged1+"=== recovery\n#1 7:merged: wait for the deployment\n" {
		t.Fatalf("no run: got\n%s", out)
	}
	p.stage("7:merged")

	// Runs of other workflows, or on other commits, do not count.
	p.fake.AddWorkflowRun("test", "main", sha, "completed", "success")
	p.fake.AddWorkflowRun("deploy", "main", "0000000", "completed", "success")
	run := p.fake.AddWorkflowRun("deploy", "main", sha, "queued", "")
	want := "=== changes\n#1 moved to 8:deploying\n=== board\n" + deploying1 + "=== recovery\n#1 8:deploying: wait for the deployment\n"
	if out := p.leadStatus(); out != want {
		t.Fatalf("queued: got\n%swant\n%s", out, want)
	}
	p.stage("8:deploying")

	p.fake.SetWorkflowRun(run, "in_progress", "")
	if out := p.leadStatus(); out != "=== board\n"+deploying1+"=== recovery\n#1 8:deploying: wait for the deployment\n" {
		t.Fatalf("in progress: got\n%s", out)
	}

	p.fake.SetWorkflowRun(run, "completed", "success")
	if out := p.leadStatus(); out != "=== changes\n#1 deployed: moved to 9:done and closed\n" {
		t.Fatalf("success: got\n%s", out)
	}
	p.stage("9:done")
	if i := p.issue(1); i.State != "closed" || i.StateReason != "completed" {
		t.Fatalf("issue %s %s", i.State, i.StateReason)
	}
	if out := p.leadStatus(); out != "nothing needs attention\n" {
		t.Fatalf("after: got\n%s", out)
	}
}

// #14-AC3: a deployment that finished before status saw it goes through
// 8:deploying to 9:done at once.
func TestStatusFinishesADeploymentAtOnce(t *testing.T) {
	p := newDevProject(t)
	sha := p.deployed("deploy.yaml")
	p.fake.AddWorkflowRun("deploy.yaml", "main", sha, "completed", "success")
	if out := p.leadStatus("--json"); out != `{"changes":[{"number":1,"change":"deployed: moved to 9:done and closed"}]}`+"\n" {
		t.Fatalf("got\n%s", out)
	}
	p.stage("9:done")
	var stages []string
	evs, err := p.gh().Events(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Event == "labeled" && strings.HasPrefix(e.Label.Name, "stage:") {
			stages = append(stages, e.Label.Name)
		}
	}
	if !slices.Contains(stages, "stage:8:deploying") {
		t.Fatalf("never deploying: %q", stages)
	}
}

// #14-AC4
func TestFailedDeployment(t *testing.T) {
	p := newDevProject(t)
	sha := p.deployed("deploy.yml")
	run := p.fake.AddWorkflowRun("deploy", "main", sha, "in_progress", "")
	p.leadStatus()
	p.refused(1, "#1 has no failed deployment", "ticket", "fix", "1", "Login deploy fails", "--lead", p.token)

	p.fake.SetWorkflowRun(run, "completed", "failure")
	if out := p.leadStatus(); out != "=== board\n"+deploying1+"=== recovery\n#1 8:deploying: triage the deployment: failure\n" {
		t.Fatalf("got\n%s", out)
	}
	p.stage("8:deploying")

	// The DM found that the code caused it.
	if out := p.ok("ticket", "fix", "1", "Login deploy fails", "--lead", p.token); out != "#3 stage=2:drafting priority=high kind=fix\n#1 stage=9:done\n" {
		t.Fatalf("got %q", out)
	}
	p.stage("9:done")
	if i := p.issue(1); i.State != "closed" || i.StateReason != "completed" {
		t.Fatalf("issue %s %s", i.State, i.StateReason)
	}
	if i := p.issue(3); i.Title != "Login deploy fails" || i.State != "open" {
		t.Fatalf("fix %+v", i)
	}
	if got := p.comments(3); len(got) != 1 || !strings.HasSuffix(got[0], "fix for the deployment of #1") {
		t.Fatalf("fix comments %q", got)
	}
	if got := p.comments(1); !strings.HasSuffix(got[len(got)-1], "deployment failed: fix in #3") {
		t.Fatalf("comments %q", got)
	}
	p.refused(1, "#1 is closed", "ticket", "fix", "1", "Again", "--lead", p.token)
}

// #14-AC4
func TestTicketFixRefusals(t *testing.T) {
	p := newDevProject(t)
	p.refused(1, "stage-changed: #1 is at 3:eligible, not 7:merged or 8:deploying", "ticket", "fix", "1", "T", "--lead", p.token)
	p.refused(1, otherLead, "ticket", "fix", "1", "T")
	p.refused(2, "missing argument: <title>", "ticket", "fix", "1", "--lead", p.token)
	sha := p.deployed("deploy.yml")
	p.refused(1, "#1 has no failed deployment", "ticket", "fix", "1", "T", "--lead", p.token)
	// A failed run that was run again and succeeded is no failure.
	run := p.fake.AddWorkflowRun("deploy", "main", sha, "completed", "failure")
	p.fake.SetWorkflowRun(run, "completed", "success")
	p.refused(1, "#1 has no failed deployment", "ticket", "fix", "1", "T", "--lead", p.token)
}

// #14-AC2
func TestStatusRemovesLeftovers(t *testing.T) {
	p := newDevProject(t)
	p.start(1, "--tier", "1")
	for _, d := range []string{"feat/7/old-work", "gate/7", "merge/9", "gate/1", "merge/1", "notes/7", "fix/x"} {
		if err := os.MkdirAll(filepath.Join(p.main, ".crew", "worktrees", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := "=== changes\n" +
		"#7 removed .crew/worktrees/feat/7/old-work crew-7 .crew/worktrees/gate/7 crew-7-gate\n" +
		"#9 removed .crew/worktrees/merge/9\n"
	if out := p.leadStatus(); !strings.HasPrefix(out, want) {
		t.Fatalf("got\n%swant prefix\n%s", out, want)
	}
	wt := filepath.Join(p.main, ".crew", "worktrees")
	calls := []string{
		filepath.Join(wt, "feat/7/old-work") + " -p crew-7 down --volumes --remove-orphans",
		filepath.Join(wt, "gate/7") + " -p crew-7-gate down --volumes --remove-orphans",
	}
	if !slices.Equal(p.box.calls, calls) {
		t.Fatalf("compose calls %q", p.box.calls)
	}
	for _, d := range []string{"feat/7", "gate/7", "merge/9"} {
		if _, err := os.Stat(filepath.Join(wt, d)); !os.IsNotExist(err) {
			t.Fatalf("%s left: %v", d, err)
		}
	}
	// Ticket 1 is at 4:in-progress, and the unknown directories are not
	// crew's.
	for _, d := range []string{worktree[len(".crew/worktrees/"):], "gate/1", "merge/1", "notes/7", "fix/x"} {
		if _, err := os.Stat(filepath.Join(wt, d)); err != nil {
			t.Fatalf("%s removed: %v", d, err)
		}
	}

	// Once cancelled, ticket 1 leaves nothing behind.
	p.ok("ticket", "cancel", "1", "--lead", p.token)
	p.box.calls = nil
	want = "=== changes\n#1 removed .crew/worktrees/feat/1/login-with-email crew-1 .crew/worktrees/gate/1 crew-1-gate .crew/worktrees/merge/1\n"
	if out := p.leadStatus(); out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	if got := p.git("worktree", "list"); strings.Contains(got, ".crew/worktrees") {
		t.Fatalf("worktrees still registered: %s", got)
	}

	// Without the lead's token, status removes nothing.
	os.MkdirAll(filepath.Join(wt, "gate", "7"), 0o755)
	p.ok("status", "--as", "dm")
	if _, err := os.Stat(filepath.Join(wt, "gate", "7")); err != nil {
		t.Fatalf("removed without the lead: %v", err)
	}
}

// #14-AC2: a failed removal stops the removals and is reported after the
// status.
func TestStatusRemovalFails(t *testing.T) {
	p := newDevProject(t)
	os.MkdirAll(filepath.Join(p.main, ".crew", "worktrees", "gate", "7"), 0o755)
	p.box.out, p.box.code = "no such project\n", 1
	code, out, errOut := p.run("status", "--as", "dm", "--lead", p.token)
	if code != 1 || !strings.HasPrefix(out, "=== board\n") || errOut != "no such project\ncrew-7-gate down: exit 1\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(p.main, ".crew", "worktrees", "gate", "7")); err != nil {
		t.Fatalf("removed after a failed down: %v", err)
	}
}

func TestStatusLeadUsage(t *testing.T) {
	p := newDevProject(t)
	p.refused(2, "--lead needs --as dm", "status", "--lead", p.token)
	p.refused(2, "--lead needs --as dm", "status", "--as", "pm", "--lead", p.token)
	p.refused(1, otherLead, "status", "--as", "dm", "--lead", "")
}
