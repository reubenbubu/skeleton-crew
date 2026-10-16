package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// relabel moves ticket n from stage from to stage to, as another command
// would.
func (p *project) relabel(n int, from, to string) {
	p.t.Helper()
	if _, err := p.gh().AddLabels(n, "stage:"+to); err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.gh().RemoveLabel(n, "stage:"+from); err != nil {
		p.t.Fatal(err)
	}
}

// rejected ends the current attempt of ticket 1 with a Reviewer's rejection.
func (p *devProject) rejected(reason string) {
	p.t.Helper()
	p.relabel(1, "4:in-progress", "4:waiting-review")
	p.post(1, "review", "reviewer", "rejected: "+reason)
	p.relabel(1, "4:waiting-review", "4:review-rejected")
}

func (p *devProject) retry(args ...string) string {
	p.t.Helper()
	return p.ok(append([]string{"ticket", "retry", "1", "--lead", p.token}, args...)...)
}

// #12-AC1
func TestRetry(t *testing.T) {
	p := newDevProject(t)
	p.start(1, "--tier", "1")
	for _, want := range []string{"2", "3", "3"} {
		p.rejected("missing coverage")
		if out := p.retry(); out != "#1 stage=4:in-progress tier="+want+"\n" {
			t.Fatalf("got %q; want tier %s", out, want)
		}
		if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"priority:medium", "kind:feat", "tier:" + want, "stage:4:in-progress"}) {
			t.Fatalf("labels %q", got)
		}
	}
}

// #12-AC1
func TestRetryJSON(t *testing.T) {
	p := newDevProject(t)
	p.start(1, "--tier", "2")
	p.rejected("x")
	if out := p.retry("--json"); out != `{"number":1,"stage":"4:in-progress","tier":3}`+"\n" {
		t.Fatalf("got %q", out)
	}
}

// #12-AC1
func TestRetryRefusals(t *testing.T) {
	p := newDevProject(t)
	p.refused(1, "stage-changed: #1 is at 3:eligible, not 4:review-rejected", "ticket", "retry", "1", "--lead", p.token)
	p.start(1, "--tier", "1")
	p.rejected("x")
	p.refused(1, otherLead, "ticket", "retry", "1")
	p.refused(2, "missing argument: <n>", "ticket", "retry", "--lead", p.token)
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:4:review-rejected") {
		t.Fatalf("labels %q", got)
	}
}

// #12-AC2
func TestRetryEscalationLimit(t *testing.T) {
	p := newDevProject(t)
	p.start(1, "--tier", "2")
	p.rejected("missing coverage")
	p.retry()
	p.relabel(1, "4:in-progress", "4:review-rejected")
	p.post(1, "log", "dev", "stuck: the session table has no email column")
	p.retry()
	p.rejected("tests time out\nin CI")
	before := len(p.comments(1))

	p.refused(1, "escalation-limit: #1 had 2 attempts at tier 3, the max_tier; blocked with a question for the user", "ticket", "retry", "1", "--lead", p.token)
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"priority:medium", "kind:feat", "tier:3", "stage:4:review-rejected", "blocked"}) {
		t.Fatalf("labels %q", got)
	}
	cs := p.comments(1)
	want := "<!-- crew:question dm -->\n" +
		"#1 failed 2 attempts at tier 3, the max_tier. How should it go on: rethink the approach, split it, or drop it?\n" +
		"attempts: tier 2, tier 3, tier 3\n" +
		"rejected: missing coverage\n" +
		"stuck: the session table has no email column\n" +
		"rejected: tests time out\n"
	if len(cs) != before+1 || cs[before] != want {
		t.Fatalf("comments %q; want %q", cs[before:], want)
	}
	// The question shows to the user.
	if out := p.ok("status"); !strings.Contains(out, "#1 blocked since 2026-01-01T00:00Z: #1 failed 2 attempts at tier 3") {
		t.Fatalf("status:\n%s", out)
	}

	// A blocked ticket is not retried, and asks nothing twice.
	p.refused(1, "blocked: #1 waits for the user; crew ticket unblock 1 once they have answered", "ticket", "retry", "1", "--lead", p.token)
	if got := len(p.comments(1)); got != before+1 {
		t.Fatalf("%d comments", got)
	}

	// Clearing blocked gives a new budget at max_tier.
	p.ok("ticket", "unblock", "1")
	p.retry()
	p.rejected("again")
	p.retry()
	p.rejected("again")
	p.failed(1, "escalation-limit: #1 had 2 attempts at tier 3, the max_tier; blocked with a question for the user", "ticket", "retry", "1", "--lead", p.token)
}

func (p *devProject) appeal(args ...string) {
	p.t.Helper()
	p.ok(append([]string{"ticket", "appeal", "1", "--lead", p.token}, args...)...)
}

// #12-AC3
func TestAppeal(t *testing.T) {
	p := newDevProject(t)
	p.start(1, "--tier", "2")
	p.rejected("too many tests")
	if out := p.ok("ticket", "appeal", "1", "The tests match the standard level.", "--lead", p.token); out != "#1 stage=4:waiting-review\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"priority:medium", "kind:feat", "tier:2", "stage:4:waiting-review"}) {
		t.Fatalf("labels %q", got)
	}
	cs := p.comments(1)
	if got := cs[len(cs)-1]; got != "<!-- crew:review dm -->\nappeal: The tests match the standard level." {
		t.Fatalf("comment %q", got)
	}

	// One appeal per attempt: the second rejection stands.
	p.post(1, "review", "reviewer", "rejected: still too many")
	p.relabel(1, "4:waiting-review", "4:review-rejected")
	p.refused(1, "#1 had its one appeal in this attempt", "ticket", "appeal", "1", "again", "--lead", p.token)

	// A new attempt allows a new appeal.
	p.retry()
	p.rejected("x")
	if out := p.ok("ticket", "appeal", "1", "--json", "y", "--lead", p.token); out != `{"number":1,"stage":"4:waiting-review"}`+"\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "tier:3") {
		t.Fatalf("labels %q", got)
	}
}

// #12-AC3
func TestAppealRefusals(t *testing.T) {
	p := newDevProject(t)
	p.start(1, "--tier", "2")
	p.refused(1, "stage-changed: #1 is at 4:in-progress, not 4:review-rejected", "ticket", "appeal", "1", "r", "--lead", p.token)
	p.relabel(1, "4:in-progress", "4:review-rejected")
	p.refused(1, "#1 has no review rejection to appeal in this attempt", "ticket", "appeal", "1", "r", "--lead", p.token)
	p.refused(1, otherLead, "ticket", "appeal", "1", "r")
	p.refused(2, "missing argument: <reason>", "ticket", "appeal", "1", "--lead", p.token)
}

// #12-AC4
func TestSplit(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.newIssue("Requirement")
	p.newIssue("Checkout", "stage:4:review-rejected", "priority:high", "kind:fix", "tier:3")
	p.subIssue(1, 2)

	out := p.ok("ticket", "split", "2", "Card payments", "Invoices", "--lead", token)
	want := "#3 stage=2:drafting priority=high kind=fix\n" +
		"#4 stage=2:drafting priority=high kind=fix\n" +
		"#2 stage=0:cancelled\n"
	if out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	for _, n := range []int{3, 4} {
		if i := p.issue(n); i.Body != skeleton {
			t.Fatalf("#%d body %q", n, i.Body)
		}
		if got := p.fake.IssueLabels(n); !slices.Equal(got, []string{"stage:2:drafting", "priority:high", "kind:fix"}) {
			t.Fatalf("#%d labels %q", n, got)
		}
		if cs, _ := p.gh().Comments(n); len(cs) != 1 || cs[0].Body != "<!-- crew:log dm -->\nsplit from #2" {
			t.Fatalf("#%d comments %v", n, cs)
		}
	}
	if i := p.issue(2); i.State != "closed" || i.StateReason != "not_planned" {
		t.Fatalf("#2 %s %s", i.State, i.StateReason)
	}
	if got := p.fake.IssueLabels(2); !slices.Equal(got, []string{"priority:high", "kind:fix", "tier:3", "stage:0:cancelled"}) {
		t.Fatalf("#2 labels %q", got)
	}
	if cs, _ := p.gh().Comments(2); len(cs) != 1 || cs[0].Body != "<!-- crew:log dm -->\nsplit into #3 #4" {
		t.Fatalf("#2 comments %v", cs)
	}
	subs, _ := p.gh().SubIssues(1)
	var ns []int
	for _, s := range subs {
		ns = append(ns, s.Number)
	}
	if !slices.Equal(ns, []int{2, 3, 4}) {
		t.Fatalf("sub-issues of #1: %v", ns)
	}

	// A ticket created by a split is not split again.
	before := len(p.writes())
	p.refused(1, "#3 was split from #2 and cannot be split again", "ticket", "split", "3", "a", "b", "--lead", token)
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("writes %q", w)
	}
}

// #12-AC4
func TestSplitRefusals(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.newIssue("Checkout", staged("3:eligible")...)
	p.refused(2, "split needs at least two titles", "ticket", "split", "1", "a", "--lead", token)
	p.refused(2, "missing argument: <title>", "ticket", "split", "1", "--lead", token)
	p.refused(1, otherLead, "ticket", "split", "1", "a", "b")
	p.refused(1, "#9 does not exist", "ticket", "split", "9", "a", "b", "--lead", token)
	p.newIssue("Done", staged("9:done")...)
	p.close(2, "completed")
	p.refused(1, "#2 is closed", "ticket", "split", "2", "a", "b", "--lead", token)
}

// #12-AC5
func TestCancel(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	for _, stage := range []string{"1:intake", "3:eligible", "4:in-progress", "6:pending-merge", "8:deploying"} {
		n := p.newIssue("T", staged(stage)...)
		if out := p.ok("ticket", "cancel", fmt.Sprint(n), "--lead", token); out != fmt.Sprintf("#%d stage=0:cancelled\n", n) {
			t.Fatalf("got %q", out)
		}
		if i := p.issue(n); i.State != "closed" || i.StateReason != "not_planned" {
			t.Fatalf("#%d %s %s", n, i.State, i.StateReason)
		}
		if got := p.fake.IssueLabels(n); !slices.Equal(got, []string{"priority:medium", "kind:feat", "stage:0:cancelled"}) {
			t.Fatalf("#%d labels %q", n, got)
		}
	}
	// A cancel that stopped before closing closes when run again.
	n := p.newIssue("T", staged("0:cancelled")...)
	if out := p.ok("ticket", "cancel", fmt.Sprint(n), "--lead", token, "--json"); out != fmt.Sprintf(`{"number":%d,"stage":"0:cancelled"}`+"\n", n) {
		t.Fatalf("got %q", out)
	}
	if i := p.issue(n); i.State != "closed" || i.StateReason != "not_planned" {
		t.Fatalf("#%d %s %s", n, i.State, i.StateReason)
	}
}

// #12-AC5
func TestCancelRefusals(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.newIssue("Parent")
	p.refused(1, "#1 has no stage", "ticket", "cancel", "1", "--lead", token)
	p.newIssue("Done", staged("9:done")...)
	p.close(2, "completed")
	p.refused(1, "#2 is closed", "ticket", "cancel", "2", "--lead", token)
	p.newIssue("Open but done", staged("9:done")...)
	p.refused(1, "stage-changed: #3 is at 9:done, not an open stage", "ticket", "cancel", "3", "--lead", token)
	p.newIssue("Two", "stage:3:eligible", "stage:4:in-progress")
	p.refused(1, "stage-changed: #4 is at 3:eligible,4:in-progress, not an open stage", "ticket", "cancel", "4", "--lead", token)
	p.refused(1, otherLead, "ticket", "cancel", "4")
}

// writeBody writes a body file in the project and returns its path.
func (p *project) writeBody(body string) string {
	p.t.Helper()
	path := filepath.Join(p.t.TempDir(), "body.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		p.t.Fatal(err)
	}
	return path
}

// #12-AC6
func TestEdit(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Login", staged("4:in-progress", "testing:minimal")...)
	p.newIssue("Sessions", staged("9:done")...)
	p.newIssue("Mail", staged("2:drafting")...)
	p.blockedBy(1, 3)

	out := p.ok("ticket", "edit", "1", "--priority", "high", "--kind", "fix", "--testing", "thorough", "--add-dependency", "2", "--add-dependency", "2", "--remove-dependency", "3", "--remove-dependency", "3")
	if out != "#1 stage=4:in-progress priority=high kind=fix testing=thorough\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"stage:4:in-progress", "priority:high", "kind:fix", "testing:thorough"}) {
		t.Fatalf("labels %q", got)
	}
	deps, _ := p.gh().BlockedBy(1)
	if len(deps) != 1 || deps[0].Number != 2 {
		t.Fatalf("dependencies %v", deps)
	}

	p.ok("ticket", "edit", "1", "--testing", "project")
	if got := p.fake.IssueLabels(1); slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, "testing:") }) {
		t.Fatalf("labels %q", got)
	}

	body := "## Goal\nLog in.\n## Acceptance criteria\n- [#1-AC1] A known email gets a link.\n"
	if out := p.ok("ticket", "edit", "1", "--body-file", p.writeBody(body)); out != "#1 stage=4:in-progress priority=high kind=fix\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.issue(1).Body; got != body {
		t.Fatalf("body %q", got)
	}
}

// #12-AC6
func TestEditMarksRevisedCriteria(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Login", staged("4:in-progress")...)
	p.setBody(1, "## Goal\nLog in.\n## Acceptance criteria\n"+
		"- [#1-AC1] A known email gets a link.\n"+
		"- [#1-AC2] (revised 2025-12-01T08:00Z) An unknown email gets an error.\n"+
		"- [#1-AC3] Links expire.\n"+
		"## Risks\n- [#1-AC1] Not a criterion here.\n")
	p.clock.Advance(90 * time.Minute)

	edited := "## Goal\nLog in by email.\n## Acceptance criteria\n" +
		"- [#1-AC1] A known email gets a link within a minute.\n" +
		"- [#1-AC2] An unknown email gets an error.\n" +
		"- [#1-AC3] (revised 2020-01-01T00:00Z) Links expire after an hour.\n" +
		"- [#1-AC4] (revised 2020-01-01T00:00Z) Links work once.\n" +
		"## Risks\n- [#1-AC1] Changed, but not a criterion here.\n"
	out := p.ok("ticket", "edit", "1", "--body-file", p.writeBody(edited))
	if want := "#1 stage=4:in-progress priority=medium kind=feat\nrevised #1-AC1\nrevised #1-AC3\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	want := "## Goal\nLog in by email.\n## Acceptance criteria\n" +
		"- [#1-AC1] (revised 2026-01-01T01:30Z) A known email gets a link within a minute.\n" +
		"- [#1-AC2] (revised 2025-12-01T08:00Z) An unknown email gets an error.\n" +
		"- [#1-AC3] (revised 2026-01-01T01:30Z) Links expire after an hour.\n" +
		"- [#1-AC4] Links work once.\n" +
		"## Risks\n- [#1-AC1] Changed, but not a criterion here.\n"
	if got := p.issue(1).Body; got != want {
		t.Fatalf("body\n%s\nwant\n%s", got, want)
	}
}

// #12-AC6
func TestEditRefusals(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Login", staged("2:drafting")...)
	p.newIssue("Mail", staged("2:drafting")...)
	p.blockedBy(1, 2)
	before := len(p.writes())
	for _, c := range []struct {
		code int
		msg  string
		args []string
	}{
		{2, "nothing to edit", nil},
		{2, "invalid --priority: urgent; want low, medium, high or critical", []string{"--priority", "urgent"}},
		{2, "invalid --kind: bug; want feat, fix or chore", []string{"--kind", "bug"}},
		{2, "invalid --testing: lots; want minimal, standard, thorough or project", []string{"--testing", "lots"}},
		{2, "invalid ticket number: x", []string{"--add-dependency", "x"}},
		{1, "#1 already depends on #2", []string{"--priority", "high", "--add-dependency", "2"}},
		{1, "#1 does not depend on #1", []string{"--remove-dependency", "1"}},
		{1, "#9 does not exist", []string{"--priority", "high", "--add-dependency", "9"}},
		{1, "#1 cannot depend on itself", []string{"--add-dependency", "1"}},
	} {
		p.refused(c.code, c.msg, append([]string{"ticket", "edit", "1"}, c.args...)...)
	}
	c, _, errOut := p.run("ticket", "edit", "1", "--body-file", filepath.Join(t.TempDir(), "missing.md"))
	if c != 1 || !strings.Contains(errOut, "missing.md") {
		t.Fatalf("code=%d stderr=%q", c, errOut)
	}
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("writes %q", w)
	}
}

// #12-AC7
func TestNewWithParent(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Payments", staged("2:drafting")...)
	if out := p.ok("ticket", "new", "Card payments", "--parent", "1"); out != "#2 stage=2:drafting priority=medium kind=feat parent=#1\n" {
		t.Fatalf("got %q", out)
	}
	if out := p.ok("ticket", "new", "Invoices", "--parent", "1", "--json"); out != `{"number":3,"stage":"2:drafting","priority":"medium","kind":"feat","parent":1}`+"\n" {
		t.Fatalf("got %q", out)
	}
	subs, _ := p.gh().SubIssues(1)
	if len(subs) != 2 || subs[0].Number != 2 || subs[1].Number != 3 {
		t.Fatalf("sub-issues %v", subs)
	}
	// A parent keeps no stage, so status treats it as a parent.
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"priority:medium", "kind:feat"}) {
		t.Fatalf("parent labels %q", got)
	}
	if out := p.ok("status", "--as", "pm"); strings.Contains(out, "#1 ") {
		t.Fatalf("status shows the parent:\n%s", out)
	}
}

// #12-AC7
func TestNewWithParentRefusals(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Eligible", staged("3:eligible")...)
	p.newIssue("Done", staged("9:done")...)
	p.close(2, "completed")
	before := len(p.writes())
	p.refused(1, "#1 is at 3:eligible; a parent must be at 1:intake or 2:drafting", "ticket", "new", "x", "--parent", "1")
	p.refused(1, "#2 is closed", "ticket", "new", "x", "--parent", "2")
	p.refused(1, "#9 does not exist", "ticket", "new", "x", "--parent", "9")
	p.refused(2, "invalid ticket number: y", "ticket", "new", "x", "--parent", "y")
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("writes %q", w)
	}
}

// #12-AC8
func TestAck(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Login", staged("4:in-progress")...)
	p.post(1, "decision", "dm", "Reuse the session table.")
	if out := p.ok("ticket", "ack", "1"); out != "#1 acknowledged\n" {
		t.Fatalf("got %q", out)
	}
	if cs, _ := p.gh().Comments(1); cs[len(cs)-1].Body != "<!-- crew:log pm -->\nack" {
		t.Fatalf("comment %q", cs[len(cs)-1].Body)
	}
	if out := p.ok("status", "--as", "pm"); strings.Contains(out, "decisions") {
		t.Fatalf("status:\n%s", out)
	}
	p.refused(1, "#9 does not exist", "ticket", "ack", "9")
}

// #12-AC8
func TestBlockAndUnblock(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Checkout", staged("3:eligible")...)
	if out := p.ok("ticket", "block", "1", "--as", "dm", "Which payment provider?"); out != "#1 blocked\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, staged("3:eligible", "blocked")) {
		t.Fatalf("labels %q", got)
	}
	if cs, _ := p.gh().Comments(1); len(cs) != 1 || cs[0].Body != "<!-- crew:question dm -->\nWhich payment provider?" {
		t.Fatalf("comments %v", cs)
	}
	if out := p.ok("status"); out != "=== waiting for the user\n#1 blocked since 2026-01-01T00:00Z: Which payment provider?\n" {
		t.Fatalf("status:\n%s", out)
	}
	if out := p.ok("ticket", "unblock", "1"); out != "#1 unblocked\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, staged("3:eligible")) {
		t.Fatalf("labels %q", got)
	}
	p.refused(1, "#1 is not blocked", "ticket", "unblock", "1")
	if out := p.ok("ticket", "block", "1", "--as", "pm", "--json", "Ask the user"); out != `{"number":1,"blocked":true}`+"\n" {
		t.Fatalf("got %q", out)
	}
	if out := p.ok("ticket", "unblock", "1", "--json"); out != `{"number":1,"blocked":false}`+"\n" {
		t.Fatalf("got %q", out)
	}

	p.refused(2, "missing flag: --as", "ticket", "block", "1", "r")
	p.refused(2, "invalid --as: dev; want pm or dm", "ticket", "block", "1", "--as", "dev", "r")
	p.refused(2, "missing argument: <reason>", "ticket", "block", "1", "--as", "dm")
	p.refused(1, "#9 does not exist", "ticket", "block", "9", "--as", "dm", "r")
}
