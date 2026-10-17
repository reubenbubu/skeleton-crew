package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/lead"
	"github.com/reubenbubu/skeleton-crew/internal/oplock"
)

const (
	held      = "lead held since 2026-01-01T00:00Z; crew dm claim --takeover takes it once the user confirms"
	otherLead = "not-lead: the lead was claimed at 2026-01-01T00:00Z by another session"
	noLead    = "not-lead: no lead; run crew dm claim"
)

var claimed = regexp.MustCompile(`^lead ([0-9a-f]{32}) claimed 2026-01-01T00:00Z\n$`)

// claim runs crew dm claim with args and returns the token it prints.
func (p *project) claim(args ...string) string {
	p.t.Helper()
	out := p.ok(append([]string{"dm", "claim"}, args...)...)
	m := claimed.FindStringSubmatch(out)
	if m == nil {
		p.t.Fatalf("crew dm claim: got %q", out)
	}
	return m[1]
}

// standIn is a lead-only command for the tests, as start, retry, appeal,
// split, merge and cancel are.
func standIn(args []string, env Env) int {
	fs := flag.NewFlagSet("stand-in", flag.ContinueOnError)
	token := leadFlag(fs)
	err := flags(fs, args)
	if err == nil {
		err = checkLead(env, *token)
	}
	if err == nil {
		err = lines(env.Stdout, "ran")
	}
	return finish(env, err, false)
}

func withStandIn(t *testing.T) {
	commands["stand-in"] = standIn
	t.Cleanup(func() { delete(commands, "stand-in") })
}

// #6-AC1 #6-AC7
func TestClaim(t *testing.T) {
	p := newTicketProject(t)
	head := p.git("rev-parse", "HEAD")
	token := p.claim()
	if got, want := p.read(".crew/lead.lock"), fmt.Sprintf("token = %q\nclaimed = \"2026-01-01T00:00:00Z\"\n", token); got != want {
		t.Fatalf("lock %q; want %q", got, want)
	}
	p.refused(1, held, "dm", "claim")
	if got := p.read(".crew/lead.lock"); !strings.Contains(got, token) {
		t.Fatalf("a refused claim changed the lock: %q", got)
	}
	if got := p.git("rev-parse", "HEAD"); got != head {
		t.Fatal("claim committed")
	}
}

func TestClaimJSON(t *testing.T) {
	p := newTicketProject(t)
	out := p.ok("dm", "claim", "--json")
	if !regexp.MustCompile(`^\{"token":"[0-9a-f]{32}","claimed":"2026-01-01T00:00:00Z"\}\n$`).MatchString(out) {
		t.Fatalf("got %q", out)
	}
}

// #6-AC7: every DM session of a project shares the lock in the main
// checkout, wherever in the project it runs.
func TestLockIsInTheMainCheckout(t *testing.T) {
	p := newTicketProject(t)
	p.git("worktree", "add", "-q", "-b", "other", filepath.Join(p.dir, ".crew", "worktrees", "other"))
	p.write("sub/x", "")
	for _, dir := range []string{filepath.Join(p.dir, "sub"), filepath.Join(p.dir, ".crew", "worktrees", "other")} {
		main := p.dir
		p.dir = dir
		token := p.claim()
		p.dir = main
		if !strings.Contains(p.read(".crew/lead.lock"), token) {
			t.Fatalf("%s: lock not in the main checkout", dir)
		}
		p.ok("dm", "release", "--lead", token)
	}
}

// #6-AC2
func TestTakeover(t *testing.T) {
	p := newTicketProject(t)
	withStandIn(t)
	old := p.claim()
	token := p.claim("--takeover")
	if token == old {
		t.Fatal("takeover kept the token")
	}
	p.refused(1, otherLead, "stand-in", "--lead", old)
	p.refused(1, otherLead, "dm", "release", "--lead", old)
	if out := p.ok("stand-in", "--lead", token); out != "ran\n" {
		t.Fatalf("got %q", out)
	}
	// A lead that has lost its token takes over from itself.
	if again := p.claim("--takeover"); again == token {
		t.Fatal("takeover kept the token")
	}
	p.refused(1, otherLead, "stand-in", "--lead", token)
}

func TestTakeoverWithNoLead(t *testing.T) {
	p := newTicketProject(t)
	p.claim("--takeover")
}

// #6-AC3
func TestLeadOnlyCommandsNeedTheLead(t *testing.T) {
	p := newTicketProject(t)
	withStandIn(t)
	p.refused(1, noLead, "stand-in")
	p.refused(1, noLead, "stand-in", "--lead", strings.Repeat("0", 32))
	token := p.claim()
	p.refused(1, otherLead, "stand-in")
	p.refused(1, otherLead, "stand-in", "--lead", "")
	p.refused(1, otherLead, "stand-in", "--lead", strings.Repeat("0", 32))
	if out := p.ok("stand-in", "--lead", token); out != "ran\n" {
		t.Fatalf("got %q", out)
	}
}

// #6-AC4
func TestUnreadableLockIsNoLead(t *testing.T) {
	for name, damage := range map[string]func(p *project){
		"garbage":   func(p *project) { p.write(".crew/lead.lock", "\x00\x01lead") },
		"empty":     func(p *project) { p.write(".crew/lead.lock", "") },
		"no token":  func(p *project) { p.write(".crew/lead.lock", "claimed = \"2026-01-01T00:00:00Z\"\n") },
		"bad token": func(p *project) { p.write(".crew/lead.lock", "token = \"x\"\nclaimed = \"2026-01-01T00:00:00Z\"\n") },
		"bad time": func(p *project) {
			p.write(".crew/lead.lock", "token = \""+strings.Repeat("a", 32)+"\"\nclaimed = \"today\"\n")
		},
		"unknown key": func(p *project) {
			p.write(".crew/lead.lock", "token = \""+strings.Repeat("a", 32)+"\"\nclaimed = \"2026-01-01T00:00:00Z\"\nx = 1\n")
		},
		"a directory": func(p *project) { os.MkdirAll(filepath.Join(p.dir, ".crew", "lead.lock"), 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			p := newTicketProject(t)
			withStandIn(t)
			damage(p)
			code, out, errOut := p.run("stand-in", "--lead", strings.Repeat("a", 32))
			warning, refusal, _ := strings.Cut(errOut, "\n")
			if code != 1 || out != "" || refusal != noLead+"\n" || !unreadable(warning) {
				t.Fatalf("stand-in: code=%d stdout=%q stderr=%q", code, out, errOut)
			}
			code, out, errOut = p.run("dm", "claim")
			if code != 0 || !claimed.MatchString(out) || !unreadable(strings.TrimSuffix(errOut, "\n")) || strings.Count(errOut, "\n") != 1 {
				t.Fatalf("claim: code=%d stdout=%q stderr=%q", code, out, errOut)
			}
			token := claimed.FindStringSubmatch(out)[1]
			if out := p.ok("stand-in", "--lead", token); out != "ran\n" {
				t.Fatalf("got %q", out)
			}
		})
	}
}

func unreadable(line string) bool {
	return strings.HasPrefix(line, "warning: .crew/lead.lock unreadable: ") && strings.HasSuffix(line, "; counted as no lead") && !strings.Contains(line, "\n")
}

// #6-AC5
func TestRelease(t *testing.T) {
	p := newTicketProject(t)
	withStandIn(t)
	p.refused(1, noLead, "dm", "release", "--lead", strings.Repeat("0", 32))
	token := p.claim()
	p.refused(1, otherLead, "dm", "release")
	if out := p.ok("dm", "release", "--lead", token); out != "lead released\n" {
		t.Fatalf("got %q", out)
	}
	if p.exists(".crew/lead.lock") {
		t.Fatal("lock left behind")
	}
	p.refused(1, noLead, "stand-in", "--lead", token)
	p.claim()
}

func TestReleaseJSON(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	if out := p.ok("dm", "release", "--lead", token, "--json"); out != `{"released":true}`+"\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDMCommandUsage(t *testing.T) {
	p := newTicketProject(t)
	p.refused(2, "usage: crew dm <command>", "dm")
	p.refused(2, "unknown command: dm bogus", "dm", "bogus")
	p.refused(2, "unknown flag: --bogus", "dm", "claim", "--bogus")
	p.refused(2, "unexpected argument: x", "dm", "release", "x")
}

// A takeover while crew merge gates stops the merge before it pushes to
// main: the old lead changes nothing more.
func TestTakeoverStopsAMerge(t *testing.T) {
	p := newDevProject(t)
	p.approved()
	other := p.pushMain("docs/notes.md", "notes\n")
	p.box.answer = func(dir string, args []string) (string, int) {
		if _, _, err := lead.Claim(p.main, p.clock.Now(), true); err != nil {
			t.Error(err)
		}
		p.box.answer = nil
		return "", 0
	}
	code, out, errOut := p.run("merge", "1", "--lead", p.token)
	if code != 1 || !strings.HasPrefix(out, "rebased ") || strings.Contains(out, "merged") || !strings.HasPrefix(errOut, "not-lead: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if got := gitIn(t, p.origin, "rev-parse", "main"); got != other {
		t.Fatalf("origin main at %s; want %s", got, other)
	}
	p.stage("6:pending-merge")
	if _, err := os.Stat(filepath.Join(p.main, worktree)); err != nil {
		t.Fatalf("worktree removed: %v", err)
	}
}

// One lead-only command at a time: another waits for the operation lock,
// and a wait for events does not.
func TestLeadCommandsWaitForTheOperationLock(t *testing.T) {
	p := newTicketProject(t)
	token := p.claim()
	path := filepath.Join(p.dir, OperationPath)
	holder, ok, err := oplock.TryLock(path, "crew merge 12 pid 99 since 2026-01-01T00:00Z")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	quiet(t, p.background("events", "--wait", "--lead", token))
	events := p.background("events", "--lead", token)
	p.clock.BlockUntil(1)
	running(t, events)
	holder.Release()
	p.clock.Advance(time.Second)
	if r := <-events; r.code != 0 || r.out != "no events\n" || r.errOut != "waiting for crew merge 12 pid 99 since 2026-01-01T00:00Z to finish\n" {
		t.Fatalf("got %+v", r)
	}
	if got := oplock.Holder(path); !strings.HasPrefix(got, "crew events pid ") {
		t.Fatalf("holder %q", got)
	}
	// Released when the command ended.
	l, ok, err := oplock.TryLock(path, "x")
	if err != nil || !ok {
		t.Fatalf("after: %v %v", ok, err)
	}
	l.Release()
}
