package cli

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// newIssue creates an issue with labels, as the user or another writer
// would, and returns its number.
func (p *project) newIssue(title string, labels ...string) int {
	p.t.Helper()
	i, err := p.gh().CreateIssue(github.NewIssue{Title: title, Labels: labels})
	if err != nil {
		p.t.Fatal(err)
	}
	return i.Number
}

// post adds a comment of kind by role to ticket n, or a human comment when
// kind is "".
func (p *project) post(n int, kind, role, text string) {
	p.t.Helper()
	body := text
	if kind != "" {
		body = ticket.Marker(kind, role) + text
	}
	if _, err := p.gh().CreateComment(n, body); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) close(n int, reason string) {
	p.t.Helper()
	if _, err := p.gh().EditIssue(n, github.IssueEdit{State: "closed", StateReason: reason}); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) subIssue(parent, child int) {
	p.t.Helper()
	if err := p.gh().AddSubIssue(parent, p.issue(child).ID); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) blockedBy(n, blocker int) {
	p.t.Helper()
	if err := p.gh().AddBlockedBy(n, p.issue(blocker).ID); err != nil {
		p.t.Fatal(err)
	}
}

func staged(stage string, extra ...string) []string {
	return append([]string{"stage:" + stage, "priority:medium", "kind:feat"}, extra...)
}

// #11-AC1
func TestStatusShowsQuestionsForTheUser(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Checkout", staged("3:eligible")...)
	p.newIssue("Login", staged("4:in-progress", "tier:2")...)
	p.newIssue("Search", staged("2:drafting")...)
	p.clock.Advance(time.Hour)
	p.post(1, "question", "dm", "Which payment provider should the checkout use?\nStripe and Adyen both fit.")
	p.gh().AddLabels(1, "blocked")
	p.clock.Advance(time.Hour)
	// Past the escalation limit, crew blocks the ticket with a question.
	p.post(2, "question", "dm", "#2 failed 2 attempts at tier 3: tests time out")
	p.post(2, "question", "dm", "Rank 3 failed again; split it or rethink?")
	p.gh().AddLabels(2, "blocked")
	p.post(3, "question", "dev", "Not blocked, so not for the user")

	want := "=== waiting for the user\n" +
		"#1 blocked since 2026-01-01T01:00Z: Which payment provider should the checkout use?\n" +
		"#2 blocked since 2026-01-01T02:00Z: Rank 3 failed again; split it or rethink?\n"
	if out := p.ok("status"); out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	for _, role := range []string{"pm", "dm"} {
		if out := p.ok("status", "--as", role); !strings.HasPrefix(out, want) {
			t.Fatalf("--as %s: got\n%s", role, out)
		}
	}

	// A blocked ticket without a question is still listed.
	p.gh().AddLabels(3, "blocked")
	if out := p.ok("status"); !strings.HasSuffix(out, "#3 blocked since 2026-01-01T02:00Z\n") {
		t.Fatalf("got\n%s", out)
	}
}

// #11-AC2
func TestStatusForThePM(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Expired links crash", "stage:1:intake", "priority:high", "kind:fix")
	p.newIssue("Login with email", staged("2:drafting")...)
	p.newIssue("Checkout", staged("4:in-progress")...)
	p.newIssue("Search", staged("9:done")...)
	p.close(4, "completed")

	p.post(3, "decision", "dm", "Too old to show")
	p.clock.Advance(31 * 24 * time.Hour)
	p.post(3, "decision", "dm", "Reuse the session table.")
	p.post(3, "log", "pm", "ack")
	p.post(3, "decision", "dm", "No new migration.\nThe old table has every column.")
	p.post(3, "decision", "pm", "Not a DM decision")
	p.post(3, "log", "dm", "ack")
	p.post(4, "decision", "dm", "Index titles only.")

	want := "=== intake\n" +
		"#1 stage=1:intake priority=high kind=fix · Expired links crash\n" +
		"=== drafting\n" +
		"#2 stage=2:drafting priority=medium kind=feat · Login with email\n" +
		"=== decisions\n" +
		"#3 2026-02-01T00:00Z: No new migration.\n" +
		"#4 2026-02-01T00:00Z: Index titles only.\n" +
		"=== stale\n" +
		"#1 1:intake since 2026-01-01T00:00Z: not triaged\n" +
		"#2 2:drafting since 2026-01-01T00:00Z: not ready\n"
	if out := p.ok("status", "--as", "pm"); out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	if out := p.ok("status"); out != "nothing needs attention\n" {
		t.Fatalf("everyone: got\n%s", out)
	}

	p.post(3, "log", "pm", "ack")
	p.post(4, "log", "pm", "ack")
	if out := p.ok("status", "--as", "pm"); strings.Contains(out, "decisions") {
		t.Fatalf("acknowledged decisions shown:\n%s", out)
	}
}

// #11-AC3
func TestStatusForTheDM(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Intake", "stage:1:intake")
	p.newIssue("Drafting", staged("2:drafting")...)
	p.newIssue("Checkout", staged("3:eligible")...)
	p.newIssue("Login", staged("4:in-progress", "tier:2")...)
	p.newIssue("Search", staged("4:in-progress", "tier:1")...)
	p.newIssue("Links", staged("4:waiting-review", "tier:1")...)
	p.newIssue("Logout", staged("4:review-rejected", "tier:1")...)
	p.newIssue("Profile", staged("5:review-approved", "tier:3")...)
	p.newIssue("Avatar", staged("6:pending-merge", "tier:1")...)
	p.newIssue("Theme", staged("7:merged", "tier:1")...)
	p.newIssue("Font", staged("8:deploying", "tier:1")...)
	p.newIssue("Export", staged("4:in-progress", "tier:1", "blocked")...)

	p.post(4, "question", "dev", "Error page or redirect?\nThe criteria do not say.")
	p.post(5, "question", "dev", "Which index?")
	p.post(5, "answer", "dm", "The title index.")
	p.post(4, "", "", "A human comment is no answer")

	want := "=== waiting for the user\n" +
		"#12 blocked since 2026-01-01T00:00Z\n" +
		"=== dev questions\n" +
		"#4 2026-01-01T00:00Z: Error page or redirect?\n" +
		"=== board\n" +
		"#3 stage=3:eligible priority=medium kind=feat · Checkout\n" +
		"#4 stage=4:in-progress priority=medium kind=feat tier=2 · Login\n" +
		"#5 stage=4:in-progress priority=medium kind=feat tier=1 · Search\n" +
		"#6 stage=4:waiting-review priority=medium kind=feat tier=1 · Links\n" +
		"#7 stage=4:review-rejected priority=medium kind=feat tier=1 · Logout\n" +
		"#8 stage=5:review-approved priority=medium kind=feat tier=3 · Profile\n" +
		"#9 stage=6:pending-merge priority=medium kind=feat tier=1 · Avatar\n" +
		"#10 stage=7:merged priority=medium kind=feat tier=1 · Theme\n" +
		"#11 stage=8:deploying priority=medium kind=feat tier=1 · Font\n" +
		"#12 stage=4:in-progress priority=medium kind=feat tier=1 blocked · Export\n" +
		"=== recovery\n" +
		"#4 4:in-progress: answer the Dev question, then start a Dev at tier 2\n" +
		"#5 4:in-progress: start a Dev at tier 1\n" +
		"#6 4:waiting-review: start a Reviewer\n" +
		"#7 4:review-rejected: retry one tier up, then start a Dev\n" +
		"#8 5:review-approved: merge\n" +
		"#9 6:pending-merge: merge again\n" +
		"#10 7:merged: advance from the deployment status\n" +
		"#11 8:deploying: advance from the deployment status\n"
	if out := p.ok("status", "--as", "dm"); out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}

	out := p.ok("status", "--as", "dm", "--json")
	for _, s := range []string{`"dev_questions":[{"number":4,"at":"2026-01-01T00:00:00Z","text":"Error page or redirect?"}]`, `"recovery":[{"number":4,"stage":"4:in-progress","action":"answer the Dev question, then start a Dev at tier 2"}`} {
		if !strings.Contains(out, s) {
			t.Fatalf("json lacks %s:\n%s", s, out)
		}
	}
}

// #11-AC4
func TestStatusFlagsStaleTickets(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Intake", "stage:1:intake", "priority:medium", "kind:feat")
	p.newIssue("Drafting", staged("2:drafting")...)
	p.newIssue("Eligible", staged("3:eligible")...)
	p.newIssue("Working", staged("4:in-progress", "tier:1")...)
	p.newIssue("Review", staged("4:waiting-review", "tier:1")...)
	p.newIssue("Blocked", staged("4:waiting-review", "tier:1", "blocked")...)
	p.clock.Advance(30 * time.Minute)
	// #5 reached its stage again later, so it counts from then.
	p.fake.SetIssueLabels(5, staged("4:in-progress", "tier:1")...)
	p.fake.SetIssueLabels(5, staged("4:waiting-review", "tier:1")...)

	dm := func() string {
		out := p.ok("status", "--as", "dm")
		_, stale, _ := strings.Cut(out, "=== stale\n")
		stale, _, _ = strings.Cut(stale, "===")
		return stale
	}
	p.clock.Advance(31 * time.Minute)
	if got := dm(); got != "" {
		t.Fatalf("stale after 1h01m:\n%s", got)
	}
	p.clock.Advance(30 * time.Minute)
	if got, want := dm(), "#5 4:waiting-review since 2026-01-01T00:30Z: no reviewer\n"; got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
	p.clock.Advance(3 * time.Hour)
	if got, want := dm(), "#4 4:in-progress since 2026-01-01T00:00Z: not submitted\n#5 4:waiting-review since 2026-01-01T00:30Z: no reviewer\n"; got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
	p.clock.Advance(20 * time.Hour)
	if got, want := dm(), "#3 3:eligible since 2026-01-01T00:00Z: not started\n#4 4:in-progress since 2026-01-01T00:00Z: not submitted\n#5 4:waiting-review since 2026-01-01T00:30Z: no reviewer\n"; got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
	pm := p.ok("status", "--as", "pm")
	if _, stale, _ := strings.Cut(pm, "=== stale\n"); stale != "#1 1:intake since 2026-01-01T00:00Z: not triaged\n" {
		t.Fatalf("pm got\n%s", pm)
	}
	p.clock.Advance(6 * 24 * time.Hour)
	pm = p.ok("status", "--as", "pm")
	if _, stale, _ := strings.Cut(pm, "=== stale\n"); stale != "#1 1:intake since 2026-01-01T00:00Z: not triaged\n#2 2:drafting since 2026-01-01T00:00Z: not ready\n" {
		t.Fatalf("pm got\n%s", pm)
	}
}

// #11-AC5
func TestStatusLabelsNewIssuesIntake(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Expired links crash")
	p.newIssue("Dark mode", "priority:high", "kind:chore")
	p.newIssue("Parent")
	p.newIssue("Child", staged("2:drafting")...)
	p.subIssue(3, 4)

	want := "=== changes\n" +
		"#1 labelled stage=1:intake priority=medium kind=feat\n" +
		"#2 labelled stage=1:intake\n"
	if out := p.ok("status"); out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"stage:1:intake", "priority:medium", "kind:feat"}) {
		t.Fatalf("labels %q", got)
	}
	if got := p.fake.IssueLabels(2); !slices.Equal(got, []string{"priority:high", "kind:chore", "stage:1:intake"}) {
		t.Fatalf("labels %q", got)
	}
	if got := p.fake.IssueLabels(3); len(got) != 0 {
		t.Fatalf("parent labelled %q", got)
	}
	before := len(p.writes())
	if out := p.ok("status"); out != "nothing needs attention\n" {
		t.Fatalf("second run: got\n%s", out)
	}
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("second run wrote %q", w)
	}
}

// #11-AC6
func TestStatusClosesFinishedParents(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Payments")
	p.newIssue("Card", staged("9:done")...)
	p.newIssue("Invoice", staged("4:in-progress")...)
	p.newIssue("Refunds")
	p.newIssue("Partial", staged("0:cancelled")...)
	p.subIssue(1, 2)
	p.subIssue(1, 3)
	p.subIssue(4, 5)
	p.close(2, "completed")
	p.close(5, "not_planned")

	if out := p.ok("status"); out != "=== changes\n#4 closed: all sub-issues closed\n" {
		t.Fatalf("got\n%s", out)
	}
	if i := p.issue(4); i.State != "closed" || i.StateReason != "not_planned" {
		t.Fatalf("#4 %s %s", i.State, i.StateReason)
	}
	if i := p.issue(1); i.State != "open" {
		t.Fatalf("#1 closed with an open sub-issue")
	}

	p.close(3, "not_planned")
	if out := p.ok("status", "--json"); out != `{"changes":[{"number":1,"change":"closed: all sub-issues closed"}]}`+"\n" {
		t.Fatalf("got\n%s", out)
	}
	if i := p.issue(1); i.State != "closed" || i.StateReason != "completed" {
		t.Fatalf("#1 %s %s", i.State, i.StateReason)
	}
}

func TestStatusUsage(t *testing.T) {
	p := newTicketProject(t)
	p.refused(2, "invalid --as: dev; want pm or dm", "status", "--as", "dev")
	p.refused(2, "unexpected argument: x", "status", "x")
}

// #11-AC7
func TestTicketNext(t *testing.T) {
	p := newTicketProject(t)
	p.newIssue("Low", "stage:3:eligible", "priority:low", "kind:feat")
	p.newIssue("Old medium", staged("3:eligible")...)
	p.clock.Advance(time.Minute)
	p.newIssue("High", "stage:3:eligible", "priority:high", "kind:fix")
	p.newIssue("New medium", staged("3:eligible")...)
	p.newIssue("Critical", "stage:3:eligible", "priority:critical", "kind:feat")
	p.newIssue("Blocked", "stage:3:eligible", "priority:critical", "kind:feat", "blocked")
	p.newIssue("Drafting", "stage:2:drafting", "priority:critical", "kind:feat")
	p.newIssue("Waits", "stage:3:eligible", "priority:critical", "kind:feat")
	p.newIssue("Dependency done", staged("3:eligible")...)
	p.newIssue("Dependency cancelled", staged("3:eligible")...)
	p.newIssue("Done", staged("9:done")...)
	p.newIssue("Cancelled", staged("0:cancelled")...)
	p.blockedBy(8, 4)
	p.blockedBy(9, 11)
	p.blockedBy(10, 12)
	p.close(11, "completed")
	p.close(12, "not_planned")

	want := "#5 stage=3:eligible priority=critical kind=feat · Critical\n" +
		"#3 stage=3:eligible priority=high kind=fix · High\n" +
		"#2 stage=3:eligible priority=medium kind=feat · Old medium\n" +
		"#4 stage=3:eligible priority=medium kind=feat · New medium\n" +
		"#9 stage=3:eligible priority=medium kind=feat · Dependency done\n" +
		"#1 stage=3:eligible priority=low kind=feat · Low\n"
	if out := p.ok("ticket", "next"); out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	out := p.ok("ticket", "next", "--json")
	if !strings.HasPrefix(out, `{"tickets":[{"number":5,"title":"Critical","stage":"3:eligible","priority":"critical","kind":"feat","blocked":false},`) {
		t.Fatalf("json %s", out)
	}
	before := len(p.writes())
	p.ok("ticket", "next")
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("writes %q", w)
	}
}
