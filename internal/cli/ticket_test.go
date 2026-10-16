package cli

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

// skeleton is the body crew ticket new writes.
const skeleton = "## Goal\n\n## Acceptance criteria\n\n## Out of scope\n\n## Assumptions\n\n## Non-functional\n\n## Risks\n"

// newTicketProject returns a clone of a GitHub repository that has crew's
// labels.
func newTicketProject(t *testing.T) *project {
	t.Helper()
	p := newProject(t, "user/app")
	p.existing()
	for _, l := range labels.All([]int{1, 2, 3}) {
		if err := p.gh().CreateLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// gh is a client for the fake GitHub, for a test that plays another writer.
func (p *project) gh() *github.Client { return github.New(p.fullName, p.fake.Run) }

// ok runs crew and fails the test unless it succeeds with terse output.
func (p *project) ok(args ...string) string {
	p.t.Helper()
	code, out, errOut := p.run(args...)
	if code != 0 || errOut != "" {
		p.t.Fatalf("crew %s: code=%d stderr=%q stdout=%q", strings.Join(args, " "), code, errOut, out)
	}
	outputtest.CheckTerse(p.t, out)
	return out
}

// refused runs crew and fails the test unless it exits with code and prints
// only the line msg on stderr.
func (p *project) refused(code int, msg string, args ...string) {
	p.t.Helper()
	c, out, errOut := p.run(args...)
	if c != code || out != "" || errOut != msg+"\n" {
		p.t.Fatalf("crew %s: got code=%d stdout=%q stderr=%q; want code=%d stderr=%q", strings.Join(args, " "), c, out, errOut, code, msg)
	}
}

func (p *project) issue(n int) github.Issue {
	p.t.Helper()
	i, err := p.gh().Issue(n)
	if err != nil {
		p.t.Fatal(err)
	}
	return i
}

func (p *project) setBody(n int, body string) {
	p.t.Helper()
	if _, err := p.gh().EditIssue(n, github.IssueEdit{Body: &body}); err != nil {
		p.t.Fatal(err)
	}
}

// #5-AC1
func TestTicketNew(t *testing.T) {
	p := newTicketProject(t)
	if out := p.ok("ticket", "new", "Login with email"); out != "#1 stage=2:drafting priority=medium kind=feat\n" {
		t.Fatalf("got %q", out)
	}
	i := p.issue(1)
	if i.Title != "Login with email" || i.Body != skeleton {
		t.Fatalf("got title %q body %q", i.Title, i.Body)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"stage:2:drafting", "priority:medium", "kind:feat"}) {
		t.Fatalf("labels %q", got)
	}

	out := p.ok("ticket", "new", "--intake", "--priority", "high", "Expired links crash", "--kind", "fix", "--json")
	if out != `{"number":2,"stage":"1:intake","priority":"high","kind":"fix"}`+"\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(2); !slices.Equal(got, []string{"stage:1:intake", "priority:high", "kind:fix"}) {
		t.Fatalf("labels %q", got)
	}
}

// #5-AC1
func TestTicketNewRefusesBadInput(t *testing.T) {
	p := newTicketProject(t)
	before := len(p.writes())
	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{}, "missing argument: <title>"},
		{[]string{"  "}, "missing argument: <title>"},
		{[]string{"a", "b"}, "unexpected argument: b"},
		{[]string{"--priority", "urgent", "t"}, "invalid --priority: urgent; want low, medium, high or critical"},
		{[]string{"--kind", "bug", "t"}, "invalid --kind: bug; want feat, fix or chore"},
		{[]string{"--bogus", "t"}, "unknown flag: --bogus"},
	} {
		p.refused(2, c.msg, append([]string{"ticket", "new"}, c.args...)...)
	}
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("writes %q", w)
	}
}

func TestTicketCommandUsage(t *testing.T) {
	p := newTicketProject(t)
	p.refused(2, "usage: crew ticket <command>", "ticket")
	p.refused(2, "unknown command: ticket bogus", "ticket", "bogus")
	p.refused(2, "invalid ticket number: x", "ticket", "show", "x")
	p.refused(2, "missing argument: <n>", "ticket", "show")
}

func TestTicketNumberMustBeATicket(t *testing.T) {
	p := newTicketProject(t)
	p.fake.SetBranch("main", "a1")
	p.fake.SetBranch("feat/1/x", "b2")
	if _, err := p.gh().CreatePullRequest(github.NewPullRequest{Title: "x", Head: "feat/1/x", Base: "main"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"show", "1"}, {"triage", "1"}, {"ready", "1"}, {"comment", "1", "--kind", "log", "--as", "dm", "x"}} {
		p.refused(1, "#1 is a pull request, not a ticket", append([]string{"ticket"}, args...)...)
		p.refused(1, "#9 does not exist", append([]string{"ticket", args[0], "9"}, args[2:]...)...)
	}
}

// #5-AC2 #5-AC3 #5-AC7
func TestStageMovesOnlyForward(t *testing.T) {
	p := newTicketProject(t)
	c := p.gh()
	// One ticket at each stage, and one with no stage, each ready in content.
	for k, s := range append(slices.Clone(labels.Stages), labels.Stage{}) {
		n := k + 1
		var ls []string
		if s.Label != "" {
			ls = []string{s.Label}
		}
		body := fmt.Sprintf("## Goal\nLog in.\n## Acceptance criteria\n- [#%d-AC1] A known email gets a link.\n", n)
		if _, err := c.CreateIssue(github.NewIssue{Title: "t", Body: body, Labels: ls}); err != nil {
			t.Fatal(err)
		}
	}
	before := len(p.writes())
	for k, s := range append(slices.Clone(labels.Stages), labels.Stage{}) {
		n := k + 1
		for _, cmd := range []struct{ name, from string }{{"triage", "1:intake"}, {"ready", "2:drafting"}} {
			if s.Label == "stage:"+cmd.from {
				continue
			}
			at := "has no stage"
			if s.Label != "" {
				at = "is at " + strings.TrimPrefix(s.Label, "stage:")
			}
			p.refused(1, fmt.Sprintf("#%d %s, not %s", n, at, cmd.from), "ticket", cmd.name, fmt.Sprint(n))
		}
	}
	if w := p.writes()[before:]; len(w) != 0 {
		t.Fatalf("a refused move wrote: %q", w)
	}

	if out := p.ok("ticket", "triage", "1"); out != "#1 stage=2:drafting\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"stage:2:drafting"}) {
		t.Fatalf("labels %q", got)
	}
	if out := p.ok("ticket", "ready", "1", "--json"); out != `{"number":1,"stage":"3:eligible"}`+"\n" {
		t.Fatalf("got %q", out)
	}
	if got := p.fake.IssueLabels(1); !slices.Equal(got, []string{"stage:3:eligible"}) {
		t.Fatalf("labels %q", got)
	}
}

// #5-AC3
func TestReadyNeedsGoalAndCriteria(t *testing.T) {
	p := newTicketProject(t)
	p.ok("ticket", "new", "Login with email")
	noCriterion := "#1 not ready: no acceptance criterion such as - [#1-AC1] <text>"
	for _, c := range []struct{ body, msg string }{
		{skeleton, "#1 not ready: the goal is empty"},
		{"## Goal\n\n## Acceptance criteria\n- [#1-AC1] A known email gets a link.\n## Out of scope\nLog in.\n", "#1 not ready: the goal is empty"},
		{"## Goal\nLog in with email.\n## Acceptance criteria\n\n## Out of scope\n", noCriterion},
		{"## Goal\nLog in with email.\n## Acceptance criteria\n- [#7-AC1] A known email gets a link.\n", noCriterion},
		{"## Goal\nLog in with email.\n## Acceptance criteria\n- [#1-AC1]\n", noCriterion},
		{"## Goal\nLog in with email.\n## Out of scope\n- [#1-AC1] A known email gets a link.\n", noCriterion},
	} {
		p.setBody(1, c.body)
		p.refused(1, c.msg, "ticket", "ready", "1")
		if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:2:drafting") {
			t.Fatalf("labels %q", got)
		}
	}
	// A body edited on GitHub's website has CRLF line endings.
	p.setBody(1, "## Goal\r\nLog in with email.\r\n## Acceptance criteria\r\n- [#1-AC1] A known email gets a link.\r\n")
	if out := p.ok("ticket", "ready", "1"); out != "#1 stage=3:eligible\n" {
		t.Fatalf("got %q", out)
	}
}

// showTicket sets up a ticket with a history: stage moves, a blocked spell,
// comments of several kinds and a human comment.
func showTicket(t *testing.T) *project {
	p := newTicketProject(t)
	p.ok("ticket", "new", "--priority", "high", "Login with email")
	p.setBody(1, "## Goal\nLog in with email.\n## Acceptance criteria\n- [#1-AC1] A known email gets a link.")
	step := func() { p.clock.Advance(5 * time.Minute) }
	step()
	p.ok("ticket", "ready", "1")
	step()
	p.gh().AddLabels(1, "blocked")
	step()
	p.ok("ticket", "comment", "1", "--kind", "decision", "--as", "dm", "Reuse the session table; no new migration.")
	step()
	p.ok("ticket", "comment", "1", "--kind", "log", "--as", "dm", "Skipped #2: collides with #3.\nThe slot stays idle.")
	step()
	p.ok("ticket", "comment", "1", "--as", "dev", "--kind", "question", "Should expired links show an error page or redirect?")
	step()
	p.gh().CreateComment(1, "Redirect to login with a notice.")
	step()
	p.gh().RemoveLabel(1, "blocked")
	return p
}

const showHead = `#1 Login with email
stage=3:eligible priority=high kind=feat
---
## Goal
Log in with email.
## Acceptance criteria
- [#1-AC1] A known email gets a link.
--- timeline
`

// #5-AC4 #5-AC5
func TestTicketShow(t *testing.T) {
	p := showTicket(t)
	want := showHead + `2026-01-01T00:00Z drafting · 2026-01-01T00:05Z eligible · 2026-01-01T00:10Z blocked · 2026-01-01T00:20Z log: Skipped #2: collides with #3. · 2026-01-01T00:35Z unblocked
--- comments
[decision] DM 2026-01-01T00:15Z: Reuse the session table; no new migration.
[question] DEV 2026-01-01T00:25Z: Should expired links show an error page or redirect?
[human] user 2026-01-01T00:30Z: Redirect to login with a notice.
`
	if out := p.ok("ticket", "show", "1"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

// #5-AC4
func TestTicketShowAll(t *testing.T) {
	p := showTicket(t)
	want := showHead + `2026-01-01T00:00Z drafting · 2026-01-01T00:05Z eligible · 2026-01-01T00:10Z blocked · 2026-01-01T00:35Z unblocked
--- comments
[decision] DM 2026-01-01T00:15Z: Reuse the session table; no new migration.
[log] DM 2026-01-01T00:20Z: Skipped #2: collides with #3.
The slot stays idle.
[question] DEV 2026-01-01T00:25Z: Should expired links show an error page or redirect?
[human] user 2026-01-01T00:30Z: Redirect to login with a notice.
`
	if out := p.ok("ticket", "show", "--all", "1"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

// #5-AC4
func TestTicketShowJSON(t *testing.T) {
	p := showTicket(t)
	want := `{"number":1,"title":"Login with email","stage":"3:eligible","priority":"high","kind":"feat","blocked":false,` +
		`"body":"## Goal\nLog in with email.\n## Acceptance criteria\n- [#1-AC1] A known email gets a link.",` +
		`"timeline":[{"at":"2026-01-01T00:00:00Z","event":"drafting"},{"at":"2026-01-01T00:05:00Z","event":"eligible"},` +
		`{"at":"2026-01-01T00:10:00Z","event":"blocked"},{"at":"2026-01-01T00:20:00Z","event":"log","text":"Skipped #2: collides with #3."},` +
		`{"at":"2026-01-01T00:35:00Z","event":"unblocked"}],` +
		`"comments":[{"kind":"decision","role":"dm","author":"user","at":"2026-01-01T00:15:00Z","text":"Reuse the session table; no new migration."},` +
		`{"kind":"question","role":"dev","author":"user","at":"2026-01-01T00:25:00Z","text":"Should expired links show an error page or redirect?"},` +
		`{"kind":"human","author":"user","at":"2026-01-01T00:30:00Z","text":"Redirect to login with a notice."}]}` + "\n"
	if out := p.ok("ticket", "show", "1", "--json"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

// #5-AC5
func TestTicketComment(t *testing.T) {
	p := newTicketProject(t)
	p.ok("ticket", "new", "Login with email")
	if out := p.ok("ticket", "comment", "1", "--kind", "decision", "--as", "dm", "Reuse the session table."); out != "#1 decision posted\n" {
		t.Fatalf("got %q", out)
	}
	out := p.ok("ticket", "comment", "--json", "1", "--kind", "question", "--as", "dev", "Redirect?\nOr an error page?")
	if out != `{"number":1,"kind":"question","role":"dev"}`+"\n" {
		t.Fatalf("got %q", out)
	}
	cs, err := p.gh().Comments(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Body != "<!-- crew:decision dm -->\nReuse the session table." || cs[1].Body != "<!-- crew:question dev -->\nRedirect?\nOr an error page?" {
		t.Fatalf("comments %+v", cs)
	}

	for _, c := range []struct {
		args []string
		msg  string
	}{
		{[]string{"--as", "dm", "x"}, "missing flag: --kind"},
		{[]string{"--kind", "log", "x"}, "missing flag: --as"},
		{[]string{"--kind", "chat", "--as", "dm", "x"}, "invalid --kind: chat; want decision, question, answer, review or log"},
		{[]string{"--kind", "log", "--as", "user", "x"}, "invalid --as: user; want pm, dm, dev or reviewer"},
		{[]string{"--kind", "log", "--as", "dm"}, "missing argument: <text>"},
		{[]string{"--kind", "log", "--as", "dm", " \n"}, "missing argument: <text>"},
	} {
		p.refused(2, c.msg, append([]string{"ticket", "comment", "1"}, c.args...)...)
	}
}

// #5-AC6
func TestTicketList(t *testing.T) {
	p := newTicketProject(t)
	c := p.gh()
	p.ok("ticket", "new", "--priority", "high", "Login with email")
	p.ok("ticket", "new", "--intake", "--kind", "fix", "Expired links crash")
	c.CreateIssue(github.NewIssue{Title: "Auth rework"})
	c.CreateIssue(github.NewIssue{Title: "Pay by card", Labels: []string{"stage:4:in-progress", "priority:critical", "kind:feat", "tier:2", "testing:thorough", "blocked"}})
	c.CreateIssue(github.NewIssue{Title: "Old", Labels: []string{"stage:9:done"}})
	c.EditIssue(5, github.IssueEdit{State: "closed"})
	p.fake.SetBranch("main", "a1")
	p.fake.SetBranch("feat/1/login", "b2")
	if _, err := c.CreatePullRequest(github.NewPullRequest{Title: "Login", Head: "feat/1/login", Base: "main"}); err != nil {
		t.Fatal(err)
	}

	want := `#1 stage=2:drafting priority=high kind=feat · Login with email
#2 stage=1:intake priority=medium kind=fix · Expired links crash
#3 · Auth rework
#4 stage=4:in-progress priority=critical kind=feat tier=2 testing=thorough blocked · Pay by card
`
	if out := p.ok("ticket", "list"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	want = `{"tickets":[{"number":1,"title":"Login with email","stage":"2:drafting","priority":"high","kind":"feat","blocked":false},` +
		`{"number":2,"title":"Expired links crash","stage":"1:intake","priority":"medium","kind":"fix","blocked":false},` +
		`{"number":3,"title":"Auth rework","blocked":false},` +
		`{"number":4,"title":"Pay by card","stage":"4:in-progress","priority":"critical","kind":"feat","tier":"2","testing":"thorough","blocked":true}]}` + "\n"
	if out := p.ok("ticket", "list", "--json"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

// #5-AC8
func TestCommandsNeverAlterBodiesOrComments(t *testing.T) {
	p := newTicketProject(t)
	c := p.gh()
	body := "Logging in by email is broken."
	c.CreateIssue(github.NewIssue{Title: "Login", Body: body})
	c.CreateComment(1, "It fails for every address.")
	p.gh().AddLabels(1, "stage:1:intake")
	before := len(p.fake.Requests())

	p.ok("ticket", "triage", "1")
	p.ok("ticket", "show", "1")
	p.ok("ticket", "show", "1", "--all")
	p.ok("ticket", "list")
	p.ok("ticket", "comment", "1", "--kind", "question", "--as", "pm", "Which addresses?")
	p.refused(1, "#1 not ready: the goal is empty", "ticket", "ready", "1")
	p.ok("ticket", "new", "Other")

	for _, r := range p.fake.Requests()[before:] {
		if strings.HasPrefix(r, "PATCH ") || strings.Contains(r, "/comments/") {
			t.Fatalf("request %s", r)
		}
	}
	if got := p.issue(1).Body; got != body {
		t.Fatalf("body %q", got)
	}
	if cs, _ := c.Comments(1); cs[0].Body != "It fails for every address." {
		t.Fatalf("comment %q", cs[0].Body)
	}
}

// #5-AC7: a failed stage move never leaves a ticket without a stage.
func TestFailedMoveKeepsAStage(t *testing.T) {
	p := newTicketProject(t)
	p.ok("ticket", "new", "--intake", "Login")
	p.fake.Hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/1/labels") {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"message":"Server Error"}`))
			return true
		}
		return false
	}
	if code, _, _ := p.run("ticket", "triage", "1"); code != 1 {
		t.Fatalf("code %d", code)
	}
	if got := p.fake.IssueLabels(1); !slices.Contains(got, "stage:1:intake") {
		t.Fatalf("labels %q", got)
	}
}

// #5-AC4 #5-AC6: a ticket with two labels for one field still shows, and a
// move from it is refused.
func TestDuplicateLabelsAreShown(t *testing.T) {
	p := newTicketProject(t)
	p.gh().CreateIssue(github.NewIssue{Title: "Login", Labels: []string{"stage:2:drafting", "stage:3:eligible", "tier:1", "tier:2"}})
	if out := p.ok("ticket", "list"); out != "#1 stage=2:drafting,3:eligible tier=1,2 · Login\n" {
		t.Fatalf("got %q", out)
	}
	if out := p.ok("ticket", "show", "1"); !strings.HasPrefix(out, "#1 Login\nstage=2:drafting,3:eligible tier=1,2\n") {
		t.Fatalf("got %q", out)
	}
	p.refused(1, "#1 is at 2:drafting,3:eligible, not 2:drafting", "ticket", "ready", "1")
}

// #5-AC5: only a marker crew writes makes a comment a crew comment, and a
// log comment's timeline entry is its first line with text.
func TestCommentMarkers(t *testing.T) {
	p := newTicketProject(t)
	p.ok("ticket", "new", "Login")
	p.gh().CreateComment(1, "<!-- crew:chat dm -->\nHello.")
	p.gh().CreateComment(1, "<!-- crew:decision boss -->\nHello.")
	p.ok("ticket", "comment", "1", "--kind", "log", "--as", "dm", "\n\nGate failed.\nDetails.")
	want := "--- timeline\n2026-01-01T00:00Z drafting · 2026-01-01T00:00Z log: Gate failed.\n--- comments\n" +
		"[human] user 2026-01-01T00:00Z: <!-- crew:chat dm -->\nHello.\n" +
		"[human] user 2026-01-01T00:00Z: <!-- crew:decision boss -->\nHello.\n"
	if out := p.ok("ticket", "show", "1"); !strings.HasSuffix(out, want) {
		t.Fatalf("got\n%s\nwant suffix\n%s", out, want)
	}
}
