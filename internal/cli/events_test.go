package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/config"
)

// poll is how often a wait looks for events in the tests.
const poll = 30 * time.Second

type result struct {
	code        int
	out, errOut string
}

// background runs crew with args the way a harness runs a background
// command, and delivers its result when it exits.
func (p *project) background(args ...string) <-chan result {
	ch := make(chan result, 1)
	go func() {
		code, out, errOut := p.run(args...)
		ch <- result{code, out, errOut}
	}()
	return ch
}

func running(t *testing.T, ch <-chan result) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("exited: %+v", r)
	default:
	}
}

func quiet(t *testing.T, ch <-chan result) {
	t.Helper()
	if r := <-ch; r != (result{}) {
		t.Fatalf("got %+v; want exit 0 with no output", r)
	}
}

// #13-AC1 #13-AC6
func TestEventsReportWhatChangedSinceThePreviousCall(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	submitted := p.newIssue("Login", staged("4:in-progress", "tier:2")...)
	reviewed := p.newIssue("Search", staged("4:waiting-review", "tier:1")...)
	drafted := p.newIssue("Checkout", staged("2:drafting")...)
	asking := p.newIssue("Profile", staged("4:in-progress", "tier:1")...)
	waiting := p.newIssue("Payments", staged("3:eligible", "blocked")...)
	started := p.newIssue("Export", staged("3:eligible")...)
	p.newIssue("Reports", staged("4:waiting-review", "tier:1")...)
	p.newIssue("Idea", staged("1:intake")...)
	p.clock.Advance(time.Second)
	if out := p.ok("events", "--lead", token); out != "no events\n" {
		t.Fatalf("first call: %q", out)
	}

	p.clock.Advance(10 * time.Minute)
	p.relabel(submitted, "4:in-progress", "4:waiting-review")
	p.relabel(reviewed, "4:waiting-review", "5:review-approved")
	p.relabel(drafted, "2:drafting", "3:eligible")
	p.post(asking, "question", "dev", "Redirect or error page?\nBoth work.")
	p.clock.Advance(time.Minute)
	p.gh().RemoveLabel(waiting, "blocked")
	p.post(waiting, "", "", "Use Stripe.")
	p.post(asking, "answer", "pm", "Redirect.")
	// The lead's own work is no event.
	p.relabel(started, "3:eligible", "4:in-progress")
	p.post(started, "decision", "dm", "Reuse the table.")
	p.post(asking, "answer", "dm", "Redirect to login.")
	p.post(started, "log", "dev", "red: #6-AC1")
	p.clock.Advance(50 * time.Minute)

	want := "2026-01-01T00:10Z #1 reached 4:waiting-review\n" +
		"2026-01-01T00:10Z #2 reached 5:review-approved\n" +
		"2026-01-01T00:10Z #3 reached 3:eligible\n" +
		"2026-01-01T00:10Z #4 dev question: Redirect or error page?\n" +
		"2026-01-01T00:11Z #4 answer by pm: Redirect.\n" +
		"2026-01-01T00:11Z #5 unblocked\n" +
		"2026-01-01T00:11Z #5 comment by user: Use Stripe.\n" +
		"2026-01-01T01:00Z #7 stale at 4:waiting-review: no reviewer\n"
	if out := p.ok("events", "--lead", token); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if out := p.ok("events", "--lead", token); out != "no events\n" {
		t.Fatalf("second call: %q", out)
	}
	p.clock.Advance(10 * time.Minute)
	var got struct {
		Events []struct {
			At     time.Time `json:"at"`
			Number int       `json:"number"`
			What   string    `json:"what"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(p.ok("events", "--lead", token, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 || got.Events[0].Number != 1 || got.Events[0].What != "stale at 4:waiting-review: no reviewer" ||
		got.Events[1].Number != 2 || got.Events[1].What != "stale at 5:review-approved: not merged" ||
		!got.Events[1].At.Equal(time.Date(2026, 1, 1, 1, 10, 1, 0, time.UTC)) {
		t.Fatalf("got %+v", got)
	}
	if out := p.ok("events", "--lead", token, "--json"); out != "{\"events\":[]}\n" {
		t.Fatalf("got %q", out)
	}
}

func TestEventsNeedTheLead(t *testing.T) {
	p := newTicketProject(t)
	for _, args := range [][]string{{"events"}, {"events", "--wait"}, {"events", "--wait", "--test"}} {
		p.refused(1, noLead, args...)
	}
	token := p.claim()
	p.refused(1, otherLead, "events", "--wait", "--lead", strings.Repeat("0", 32))
	p.refused(2, "--test needs --wait", "events", "--test", "--lead", token)
	p.refused(2, "--json does not go with --wait", "events", "--wait", "--json", "--lead", token)
	p.refused(2, "unexpected argument: x", "events", "x", "--lead", token)
}

// #13-AC2
func TestWaitBlocksUntilSomethingIsUnread(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.newIssue("Login", staged("3:eligible")...)
	p.newIssue("Search", staged("2:drafting")...)
	// Before the first crew events nothing records what was read, so a
	// wait exits at once.
	quiet(t, p.background("events", "--wait", "--lead", token))
	p.clock.Advance(time.Second)
	p.ok("events", "--lead", token)

	wait := p.background("events", "--wait", "--lead", token)
	p.clock.BlockUntil(1)
	p.relabel(1, "3:eligible", "4:in-progress")
	p.clock.Advance(poll)
	p.clock.BlockUntil(1)
	running(t, wait)
	p.relabel(2, "2:drafting", "3:eligible")
	p.clock.Advance(poll)
	quiet(t, wait)
	// The wait marked nothing read.
	if out := p.ok("events", "--lead", token); out != "2026-01-01T00:00Z #2 reached 3:eligible\n" {
		t.Fatalf("got %q", out)
	}
}

// #13-AC3
func TestNewerWaitReplacesOlder(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.newIssue("Login", staged("2:drafting")...)
	p.clock.Advance(time.Second)
	p.ok("events", "--lead", token)

	older := p.background("events", "--wait", "--lead", token)
	p.clock.BlockUntil(1)
	newer := p.background("events", "--wait", "--lead", token)
	p.clock.BlockUntil(2)
	p.clock.Advance(poll)
	quiet(t, older)
	p.clock.BlockUntil(1)
	running(t, newer)
	p.relabel(1, "2:drafting", "3:eligible")
	p.clock.Advance(poll)
	quiet(t, newer)
}

// #13-AC4
func TestWaitTestExitsAfterAFewSeconds(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.clock.Advance(time.Second)
	p.ok("events", "--lead", token)
	older := p.background("events", "--wait", "--lead", token)
	p.clock.BlockUntil(1)

	test := p.background("events", "--wait", "--test", "--lead", token)
	p.clock.BlockUntil(2)
	p.clock.Advance(4 * time.Second)
	running(t, test)
	p.clock.Advance(time.Second)
	quiet(t, test)
	// The test replaced the older wait, and marked nothing read.
	p.clock.Advance(poll)
	quiet(t, older)
	if out := p.ok("events", "--lead", token); out != "no events\n" {
		t.Fatalf("got %q", out)
	}
}

// The read record and the wait are local files that crew init ignores.
func TestEventsStateIsLocal(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	p.clock.Advance(time.Second)
	p.ok("events", "--lead", token)
	if got := p.read(".crew/events.read"); got != "read = \"2026-01-01T00:00:01Z\"\n" {
		t.Fatalf("events.read %q", got)
	}
	// An unreadable record counts as none.
	p.write(".crew/events.read", "read = 1\n")
	code, out, errOut := p.run("events", "--lead", token)
	if code != 0 || out != "no events\n" || errOut != "warning: .crew/events.read unreadable: invalid read time; counted as no record\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if got := p.read(".crew/events.read"); got != "read = \"2026-01-01T00:00:01Z\"\n" {
		t.Fatalf("events.read %q", got)
	}
}

// headerOf returns the second line of crew brief dm.
func headerOf(p *devProject) string {
	p.t.Helper()
	return strings.SplitN(p.ok("brief", "dm"), "\n", 3)[1]
}

// #13-AC5
func TestHarnessWiringIsRecorded(t *testing.T) {
	p := newBriefProject(t)
	p.pushMain(config.Path, p.read(config.Path))
	if h := headerOf(p); h != "testing=thorough harness=unknown" {
		t.Fatalf("header %q", h)
	}
	p.env["AI_AGENT"] = "odd.harness"
	if h := headerOf(p); h != "testing=thorough harness=unknown" {
		t.Fatalf("header %q", h)
	}
	delete(p.env, "AI_AGENT")
	p.env["CLAUDECODE"] = "1"
	if h := headerOf(p); h != "testing=thorough harness=claude-code wiring=none" {
		t.Fatalf("header %q", h)
	}
	if out := p.ok("dm", "wiring", "claude-code", "--wait", "--lead", p.token); out != "recorded claude-code wiring=wait\n" {
		t.Fatalf("got %q", out)
	}
	if h := headerOf(p); h != "testing=thorough harness=claude-code wiring=wait" {
		t.Fatalf("header %q", h)
	}
	if got := gitIn(t, p.origin, "show", "main:"+config.Path); !strings.HasSuffix(got, "\n\n[harness.claude-code]\nwiring = \"wait\"") {
		t.Fatalf("crew.toml on main:\n%s", got)
	}
	if got := gitIn(t, p.origin, "log", "-1", "--format=%s", "main"); got != "Record the claude-code wiring" {
		t.Fatalf("subject %q", got)
	}

	p.env = map[string]string{"OPENCODE": "1"}
	p.write(".opencode/plugins/crew-wake.ts", "export default {}\n")
	start := "Load the crew-wake plugin; it wakes you on crew events."
	if out := p.ok("dm", "wiring", "opencode", "--own", start, "--file", ".opencode/plugins/crew-wake.ts", "--lead", p.token); out != "recorded opencode wiring=own\n" {
		t.Fatalf("got %q", out)
	}
	if h := headerOf(p); h != "testing=thorough harness=opencode wiring=own" {
		t.Fatalf("header %q", h)
	}
	if got := gitIn(t, p.origin, "show", "--name-only", "--format=", "main"); got != ".crew/crew.toml\n.opencode/plugins/crew-wake.ts" {
		t.Fatalf("committed %q", got)
	}
	cfg, err := config.Parse(config.Path, []byte(p.read(config.Path)))
	if err != nil || cfg.Harnesses["opencode"].Start != start || len(cfg.Harnesses) != 2 {
		t.Fatalf("config %+v %v", cfg.Harnesses, err)
	}
	// Recording a harness again replaces its record.
	p.ok("dm", "wiring", "opencode", "--wait", "--lead", p.token)
	if got := p.read(config.Path); strings.Count(got, "[harness.opencode]") != 1 || strings.Contains(got, "start =") {
		t.Fatalf("crew.toml:\n%s", got)
	}
	// Recording the same again changes nothing.
	p.ok("dm", "wiring", "opencode", "--wait", "--lead", p.token)
	if st := p.git("status", "--porcelain", "--", ".crew/crew.toml", ".opencode"); st != "" {
		t.Fatalf("status %q", st)
	}
}

func TestHarnessWiringRefusals(t *testing.T) {
	p := newBriefProject(t)
	p.pushMain(config.Path, p.read(config.Path))
	p.refused(1, otherLead, "dm", "wiring", "x", "--wait")
	p.refused(2, "missing argument: <harness>", "dm", "wiring", "--wait", "--lead", p.token)
	p.refused(2, "want --wait or --own", "dm", "wiring", "x", "--lead", p.token)
	p.refused(2, "want --wait or --own", "dm", "wiring", "x", "--wait", "--own", "go", "--lead", p.token)
	p.refused(2, "--file needs --own", "dm", "wiring", "x", "--wait", "--file", "a", "--lead", p.token)
	p.refused(2, "invalid harness: Claude Code; want letters, digits, - and _", "dm", "wiring", "Claude Code", "--wait", "--lead", p.token)
	p.refused(1, "no such file: missing.ts", "dm", "wiring", "x", "--own", "go", "--file", "missing.ts", "--lead", p.token)
	p.write(config.Path, p.read(config.Path)+"# mine\n")
	p.refused(1, ".crew/crew.toml has uncommitted changes", "dm", "wiring", "x", "--wait", "--lead", p.token)
	p.git("checkout", "--", config.Path)

	p.refused(2, "invalid --own: control characters", "dm", "wiring", "x", "--own", "go\x07", "--lead", p.token)
	p.refused(2, "invalid --file: ../x; want a path inside the project", "dm", "wiring", "x", "--own", "go", "--file", "../x", "--lead", p.token)

	// A push that origin refuses changes nothing, and other staged work
	// stays staged.
	p.write("notes.txt", "mine\n")
	p.git("add", "notes.txt")
	before := p.read(config.Path)
	head := p.git("rev-parse", "HEAD")
	hook := filepath.Join(p.origin, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := p.run("dm", "wiring", "x", "--wait", "--lead", p.token)
	if code != 1 || out != "" || !strings.Contains(errOut, "push") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if p.read(config.Path) != before || p.git("rev-parse", "HEAD") != head || p.git("status", "--porcelain", "--", config.Path, "notes.txt") != "A  notes.txt" {
		t.Fatalf("changed: head %s status %q", p.git("rev-parse", "HEAD"), p.git("status", "--porcelain"))
	}
}
