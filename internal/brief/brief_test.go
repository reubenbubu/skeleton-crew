package brief

import (
	"strings"
	"testing"
)

// duties lists, for each role, text its rules must hold: each command the
// README gives the role, and the hard rules it sets for it. A missing entry
// means a duty or rule the briefing leaves out.
var duties = map[string][]string{
	"common": {
		// Code search guidance.
		"code index", "own worktree", "re-index",
		// Hard rules for everyone.
		"through `crew`", "never call the GitHub API or `gh`", "credential", "UTC",
		"never decreases", "rule wins", "priority:critical`", "stops and reports",
		"crew ticket comment", "crew ticket show", "--all", "crew ticket list",
		"crew ticket block", "--as <role>", "crew ticket unblock", "crew status",
	},
	"pm": {
		"what is built and why", "technical decision",
		"crew status --as pm", "stale", "crew ticket triage", "crew ticket new", "--intake", "--parent",
		"crew ticket edit", "--body-file", "--testing project", "--add-dependency", "--remove-dependency", "crew ticket ready", "crew ticket ack",
		"- [#<n>-AC<k>]", "observable behaviour", "never renumber", "revised",
		"sub-issues", "split", "2:drafting", "product.md", "standing preferences",
		"testing:", "dependencies", "size budget",
	},
	"dm": {
		"how", "never wait for", "never write product code", "trivial merge conflicts",
		"crew dm claim", "--takeover", "confirm", "crew dm release", "--lead", "not-lead", "stage-changed",
		"conversational", "knowledge-only", "crew status --as dm", "no Dev or Reviewer of your session",
		"crew events --wait", "crew events --wait --test", "self-test", "send me any message",
		"crew events --lead", "harness=", "wiring=none", "crew dm wiring", "--own", "--file", "only after your previous wait has exited",
		"crew ticket next", "log` comment", "collide",
		"crew ticket start", "--tier", "at-cap", "crew brief dev", "crew brief reviewer", "session's model",
		"crew ticket ask", "answer", "same rank",
		"crew ticket retry", "top_attempts", "max_tier", "crew ticket unblock", "crew ticket appeal", "one appeal", "crew ticket split", "\"<title>\" \"<title>\"", "not planned",
		"crew ticket cancel", "crew merge", "6:pending-merge", "No Dev works at stage 6", "origin/<branch>", "merge-conflict", "merged <commit> into main",
		"deployment", "`fix` ticket", "`high`",
		"`git` and `gh` directly", "`decision` comment",
		"recovery", "4:in-progress", "4:waiting-review", "4:review-rejected", "5:review-approved", "7:merged", "8:deploying",
		"architecture.md", "conventions.md", "ADR", "size budget", "onboarding",
		"testing:", "dependencies",
	},
	"dev": {
		"worktree", "crew commit --red", "crew commit --green", "crew commit --refactor", "crew commit \"",
		"only test files", "fail", "names the criterion", "`#<n>-AC<k>`", "no staging", "rebase", "pushed",
		"the tests could not run", "minimal", "standard", "thorough",
		"skip", "weaken", "reason", "flag:",
		"crew env up", "crew env down", "crew env logs", "crew run", "containers", "toolchain",
		"crew ticket submit", "gate failed", ".crew/logs/",
		"crew ticket ask", "crew ticket stuck", "failed attempt",
		"architecture.md", "conventions.md", "ADR", "migration", "never edited",
		"`.crew/`", "existing worktree",
	},
	"reviewer": {
		"fresh", "crew review <n> --approve", "crew review <n> --reject", "binding",
		"crew ticket show <n> --all", "red: ", "assertion", "typo", "import",
		"missing coverage", "over-testing", "minimal", "standard", "thorough",
		"no behaviour", "weakened", "reason", "flag:", "knowledge", "migration",
		"appeal", "first rejection", "never change", "git diff",
	},
}

// #10-AC6
func TestRulesCoverEveryDuty(t *testing.T) {
	for role, want := range duties {
		text := Common
		if role != "common" {
			r, ok := Rules(role)
			if !ok {
				t.Fatalf("no rules for %s", role)
			}
			text = strings.TrimPrefix(r, Common)
		}
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s rules lack %q", role, w)
			}
		}
	}
}

func TestRulesAreTerse(t *testing.T) {
	for _, role := range Roles {
		r, _ := Rules(role)
		for _, bad := range []string{"http://", "https://", "\x1b", "\r"} {
			if strings.Contains(r, bad) {
				t.Errorf("%s rules contain %q", role, bad)
			}
		}
	}
}

func TestParseADR(t *testing.T) {
	for _, c := range []struct {
		path, text string
		want       ADR
		ok         bool
	}{
		{".crew/docs/adr/0001-python-stack.md", "# 0001 Python stack\n\nSummary: Python 3.13.\n", ADR{"0001", "Python stack", "Python 3.13.", ".crew/docs/adr/0001-python-stack.md"}, true},
		{".crew/docs/adr/0012-x.md", "# Use x\nSummary:   Short.  \n", ADR{"0012", "Use x", "Short.", ".crew/docs/adr/0012-x.md"}, true},
		{".crew/docs/adr/0003-no-title.md", "Just text.\n", ADR{"0003", "no-title", "", ".crew/docs/adr/0003-no-title.md"}, true},
		{".crew/docs/adr/0006-roadmap.md", "# 2024 roadmap\n", ADR{"0006", "2024 roadmap", "", ".crew/docs/adr/0006-roadmap.md"}, true},
		{".crew/docs/adr/0007-empty.md", "# \n# Later\n", ADR{"0007", "empty", "", ".crew/docs/adr/0007-empty.md"}, true},
		{".crew/docs/adr/README.md", "# ADRs\n", ADR{}, false},
		{".crew/docs/adr/0004-x.txt", "# X\n", ADR{}, false},
		{".crew/docs/adr/old/0005-x.md", "# X\n", ADR{}, false},
	} {
		got, ok := ParseADR(c.path, c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseADR(%q): got %+v, %v; want %+v, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}
