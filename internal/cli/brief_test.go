package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/brief"
	"github.com/reubenbubu/skeleton-crew/internal/config"
)

// newBriefProject returns a dev project whose configuration has Claude
// Code's models and the thorough testing level, and whose main holds the
// knowledge files and two ADRs.
func newBriefProject(t *testing.T) *devProject {
	t.Helper()
	p := newDevProject(t)
	p.write(config.Path, strings.Replace(string(config.Template("python", "claude-code", "release")), `level = "standard"`, `level = "thorough"`, 1))
	p.pushMain(".crew/docs/product.md", "# Product\n\nA login service.\n")
	p.pushMain(".crew/docs/architecture.md", "# Architecture\n\n  - `src/app/`: the package.\n\n")
	p.pushMain(".crew/docs/conventions.md", "# Conventions\n\nTests name their criterion.\n")
	p.pushMain(".crew/docs/adr/0002-session-table.md", "# 0002 Reuse the session table\n\nSummary: Logins reuse the session table.\n\n## Decision\n\nNo new table.\n")
	p.pushMain(".crew/docs/adr/0001-python-stack.md", "# 0001 Python stack\n\nSummary: Python 3.13 in containers.\n")
	p.pushMain(".crew/docs/adr/README.txt", "Not an ADR.\n")
	return p
}

const briefKnowledge = `=== .crew/docs/product.md
# Product

A login service.
=== .crew/docs/architecture.md
# Architecture

  - ` + "`src/app/`" + `: the package.

=== .crew/docs/conventions.md
# Conventions

Tests name their criterion.
=== .crew/docs/adr
0001 Python stack: Python 3.13 in containers.
0002 Reuse the session table: Logins reuse the session table.
`

// #10-AC1
func TestBriefPrintsTheRoleRules(t *testing.T) {
	p := newBriefProject(t)
	fields := map[string]string{
		"pm":       "testing=thorough",
		"dm":       "testing=thorough harness=unknown",
		"dev":      "testing=thorough",
		"reviewer": "tier=2 model=sonnet effort=medium testing=thorough",
	}
	seen := map[string]bool{}
	for _, role := range brief.Roles {
		rules, ok := brief.Rules(role)
		if !ok || seen[rules] || !strings.HasPrefix(rules, brief.Common) || len(rules) < len(brief.Common)+500 {
			t.Fatalf("rules of %s: ok=%v, %d bytes", role, ok, len(rules))
		}
		seen[rules] = true
		want := "brief " + role + "\n" + fields[role] + "\n=== rules\n" + rules + briefKnowledge
		if out := p.ok("brief", role); out != want {
			t.Fatalf("crew brief %s: got\n%s\nwant\n%s", role, out, want)
		}
	}
	p.refused(2, "missing argument: <role>", "brief")
	p.refused(2, "unknown role: bogus; want pm, dm, dev or reviewer", "brief", "bogus")
	p.refused(2, "invalid ticket number: x", "brief", "dev", "x")
	p.refused(2, "unexpected argument: 2", "brief", "dev", "1", "2")
}

// #10-AC2 #10-AC3 #10-AC4
func TestBriefWithATicket(t *testing.T) {
	p := newBriefProject(t)
	p.start(1, "--tier", "3")
	// The knowledge comes from origin/main, not from the main checkout.
	p.writeMain(".crew/docs/product.md", "# Product\n\nAn edit nobody pushed.\n")
	view := p.ok("ticket", "show", "1")
	want := "brief dev #1\ntier=3 model=opus effort=high testing=thorough\n=== rules\n" + rules(t, "dev") + "=== ticket\n" + view + briefKnowledge
	if out := p.ok("brief", "dev", "1"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	want = "brief pm #1\ntesting=thorough\n=== rules\n" + rules(t, "pm") + "=== ticket\n" + view + briefKnowledge
	if out := p.ok("brief", "pm", "1"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func rules(t *testing.T, role string) string {
	t.Helper()
	r, ok := brief.Rules(role)
	if !ok {
		t.Fatalf("no rules for %s", role)
	}
	return r
}

// #10-AC4
func TestBriefNamesTheModel(t *testing.T) {
	p := newBriefProject(t)
	p.refused(1, "#1 has no tier; crew ticket start sets it", "brief", "dev", "1")
	p.start(1, "--tier", "1")
	if out := p.ok("brief", "dev", "1"); !strings.HasPrefix(out, "brief dev #1\ntier=1 model=haiku effort=medium testing=thorough\n") {
		t.Fatalf("got\n%s", out)
	}
	// The Reviewer uses models.reviewer, whatever the ticket's tier.
	if out := p.ok("brief", "reviewer", "1"); !strings.HasPrefix(out, "brief reviewer #1\ntier=2 model=sonnet effort=medium testing=thorough\n") {
		t.Fatalf("got\n%s", out)
	}
	p.write(config.Path, strings.Replace(p.read(config.Path), "reviewer = 2", "reviewer = 3", 1))
	if out := p.ok("brief", "reviewer"); !strings.HasPrefix(out, "brief reviewer\ntier=3 model=opus effort=high testing=thorough\n") {
		t.Fatalf("got\n%s", out)
	}
	// An empty model means the session's model, so the briefing names none.
	p.write(config.Path, string(config.Template("python", "", "release")))
	if out := p.ok("brief", "dev", "1"); !strings.HasPrefix(out, "brief dev #1\ntier=1 effort=medium testing=standard\n") {
		t.Fatalf("got\n%s", out)
	}
	p.gh().AddLabels(1, "tier:7")
	p.refused(1, "#1 has more than one tier: 1,7", "brief", "dev", "1")
	p.gh().RemoveLabel(1, "tier:1")
	p.refused(1, "#1 has tier:7, which .crew/crew.toml does not define", "brief", "dev", "1")
}

// #10-AC5
func TestBriefNamesTheTestingLevel(t *testing.T) {
	p := newBriefProject(t)
	p.gh().AddLabels(1, "testing:minimal")
	if out := p.ok("brief", "pm", "1"); !strings.HasPrefix(out, "brief pm #1\ntesting=minimal\n") {
		t.Fatalf("got\n%s", out)
	}
	if out := p.ok("brief", "reviewer", "1"); !strings.HasPrefix(out, "brief reviewer #1\ntier=2 model=sonnet effort=medium testing=minimal\n") {
		t.Fatalf("got\n%s", out)
	}
	if out := p.ok("brief", "pm"); !strings.HasPrefix(out, "brief pm\ntesting=thorough\n") {
		t.Fatalf("got\n%s", out)
	}
	p.gh().AddLabels(1, "testing:loose")
	p.refused(1, "#1 has more than one testing level: minimal,loose", "brief", "pm", "1")
	p.gh().RemoveLabel(1, "testing:minimal")
	p.refused(1, "#1 has testing:loose; want minimal, standard or thorough", "brief", "pm", "1")
}

func TestBriefMissingKnowledge(t *testing.T) {
	p := newDevProject(t)
	want := "brief dm\ntesting=standard harness=unknown\n=== rules\n" + rules(t, "dm") +
		"=== .crew/docs/product.md: missing\n=== .crew/docs/architecture.md: missing\n=== .crew/docs/conventions.md: missing\n=== .crew/docs/adr\n"
	if out := p.ok("brief", "dm"); out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

func TestBriefJSON(t *testing.T) {
	p := newBriefProject(t)
	p.start(1, "--tier", "2")
	var got struct {
		Role    string `json:"role"`
		Ticket  int    `json:"ticket"`
		Tier    int    `json:"tier"`
		Model   string `json:"model"`
		Effort  string `json:"effort"`
		Testing string `json:"testing"`
		Rules   string `json:"rules"`
		View    struct {
			Number int `json:"number"`
		} `json:"view"`
		Docs []struct {
			Path    string `json:"path"`
			Text    string `json:"text"`
			Missing bool   `json:"missing"`
		} `json:"docs"`
		ADRs []struct {
			Number  string `json:"number"`
			Title   string `json:"title"`
			Summary string `json:"summary"`
			Path    string `json:"path"`
		} `json:"adrs"`
	}
	if err := json.Unmarshal([]byte(p.ok("brief", "dev", "1", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Role != "dev" || got.Ticket != 1 || got.Tier != 2 || got.Model != "sonnet" || got.Effort != "medium" || got.Testing != "thorough" ||
		got.Rules != rules(t, "dev") || got.View.Number != 1 || len(got.Docs) != 3 || got.Docs[0].Path != ".crew/docs/product.md" ||
		got.Docs[0].Text != "# Product\n\nA login service.\n" || got.Docs[0].Missing || len(got.ADRs) != 2 ||
		got.ADRs[1].Number != "0002" || got.ADRs[1].Title != "Reuse the session table" || got.ADRs[1].Summary != "Logins reuse the session table." ||
		got.ADRs[1].Path != ".crew/docs/adr/0002-session-table.md" {
		t.Fatalf("got %+v", got)
	}
}
