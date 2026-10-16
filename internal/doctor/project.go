package doctor

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
)

// Budget is the most bytes product.md, architecture.md and conventions.md
// may hold together.
const Budget = 32000

// File is a project knowledge file and its contents.
type File struct {
	Path, Text string
}

// Tiers checks that the ranks run from 1 upwards with no gaps, and that
// models.reviewer and models.max_tier name one of them.
func Tiers(c config.Config) Check {
	for i, t := range c.Tiers {
		if t.Rank != i+1 {
			return Failure("tiers", fmt.Sprintf("rank %d is missing; ranks run from 1 upwards with no gaps", i+1))
		}
	}
	highest := len(c.Tiers)
	for _, k := range []struct {
		key  string
		rank int
	}{{"models.reviewer", c.Reviewer}, {"models.max_tier", c.MaxTier}} {
		if k.rank > highest {
			return Failure("tiers", fmt.Sprintf("%s is %d, but the highest rank is %d", k.key, k.rank, highest))
		}
	}
	return Passed("tiers", fmt.Sprintf("1-%d", highest))
}

// Labels checks that the repository has every label crew uses, for the
// configuration's ranks.
func Labels(existing []github.Label, c config.Config) Check {
	var ranks []int
	for _, t := range c.Tiers {
		ranks = append(ranks, t.Rank)
	}
	want := labels.All(ranks)
	var missing []string
	for _, l := range want {
		// GitHub matches label names without regard to case.
		if !slices.ContainsFunc(existing, func(e github.Label) bool { return strings.EqualFold(e.Name, l.Name) }) {
			missing = append(missing, l.Name)
		}
	}
	if len(missing) > 0 {
		return Failure("labels", "missing "+strings.Join(missing, ", ")+"; run crew init")
	}
	return Passed("labels", fmt.Sprint(len(want)))
}

// Knowledge warns when the knowledge files that every briefing holds in
// full are over the budget together.
func Knowledge(files []File) Check {
	size := 0
	for _, f := range files {
		size += len(f.Text)
	}
	if size > Budget {
		return Warning("knowledge", fmt.Sprintf("%d bytes, over the budget of %d; condense product.md, architecture.md and conventions.md, moving detail into ADRs", size, Budget))
	}
	return Passed("knowledge", fmt.Sprintf("%d bytes", size))
}

// forbidden are what project knowledge may not tell an agent to do,
// because a built-in rule forbids it.
var forbidden = []struct {
	pattern *regexp.Regexp
	rule    string
}{
	{regexp.MustCompile(`\bgh\s+(api|issue|pr|label|repo|release|workflow|run)\b`), "agents work through crew, never gh"},
	{regexp.MustCompile(`\bgit\s+(commit|push|rebase|merge|cherry-pick)\b`), "crew makes every commit, push and branch"},
}

// negated matches a line that says not to do something, which never
// contradicts a rule that forbids it.
var negated = regexp.MustCompile(`(?i)\b(never|not|no|don't|avoid)\b`)

// Conflicts returns one warning for each line of project knowledge that
// tells agents to do what a built-in rule forbids: use gh, commit, push,
// rebase or merge with git, or use one of the profile's skip markers.
func Conflicts(files []File, skip []string) []Check {
	var checks []Check
	for _, f := range files {
		for i, line := range strings.Split(f.Text, "\n") {
			if negated.MatchString(line) {
				continue
			}
			at := fmt.Sprintf("%s:%d: ", f.Path, i+1)
			for _, fb := range forbidden {
				if m := fb.pattern.FindString(line); m != "" {
					checks = append(checks, Warning("rules", at+strings.Join(strings.Fields(m), " ")+"; "+fb.rule))
				}
			}
			for _, m := range skip {
				if strings.Contains(line, m) {
					checks = append(checks, Warning("rules", at+m+"; tests are never skipped or disabled"))
					break
				}
			}
		}
	}
	if len(checks) == 0 {
		return []Check{Passed("rules", "")}
	}
	return checks
}

// Harness checks that the configuration records wiring for harness, the
// harness in use, or "" when crew cannot tell. Missing wiring is a
// warning: everything works without it, and the lead DM's self-test
// records it.
func Harness(c config.Config, harness string) Check {
	if harness == "" {
		return Warning("harness", "unknown; crew cannot tell which harness runs it")
	}
	h, ok := c.Harnesses[harness]
	if !ok {
		return Warning("harness", harness+" has no wiring in "+config.Path+"; the lead DM runs the self-test at its next start")
	}
	return Passed("harness", harness+" "+h.Wiring)
}
