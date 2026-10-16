// Package config reads and writes .crew/crew.toml.
package config

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/toml"
)

// Path is where the configuration lives, relative to the project root.
const Path = ".crew/crew.toml"

// Tier is one rank of model and effort. An empty Model means the session's
// model.
type Tier struct {
	Rank                int
	Name, Model, Effort string
}

type Config struct {
	Profile     string
	Tiers       []Tier // by rank
	Reviewer    int
	MaxTier     int
	TopAttempts int
	Testing     string
	Devs        int
}

var (
	tierTable = regexp.MustCompile(`^tiers\.([1-9][0-9]*)$`)
	tierKey   = regexp.MustCompile(`^tiers\.([1-9][0-9]*)\.(name|model|effort)$`)
	tables    = []string{"tiers", "models", "escalation", "testing", "parallel"}
	counts    = []string{"models.reviewer", "models.max_tier", "escalation.top_attempts", "parallel.devs"}
	efforts   = []string{"low", "medium", "high", "max"}
	Levels    = []string{"minimal", "standard", "thorough"}
)

// Parse reads the configuration file called name, with contents b, and
// fills in the defaults for keys that are left out.
func Parse(name string, b []byte) (Config, error) {
	doc, err := toml.Parse(name, b)
	if err != nil {
		return Config{}, err
	}
	fail := func(line int, format string, a ...any) (Config, error) {
		return Config{}, &toml.Error{File: name, Line: line, Msg: fmt.Sprintf(format, a...)}
	}

	// Check every table and key in file order, so the first problem is the
	// one reported.
	type item struct {
		line  int
		key   string
		table bool
	}
	var items []item
	for _, t := range doc.Tables {
		items = append(items, item{doc.TableLines[t], t, true})
	}
	for k, v := range doc.Values {
		items = append(items, item{v.Line, k, false})
	}
	slices.SortFunc(items, func(a, b item) int { return cmp.Compare(a.line, b.line) })

	c := Config{TopAttempts: 2, Testing: "standard", Devs: 3}
	tiers := map[int]*Tier{}
	tier := func(rank string) *Tier {
		n, _ := strconv.Atoi(rank)
		if tiers[n] == nil {
			tiers[n] = &Tier{Rank: n}
		}
		return tiers[n]
	}
	for _, it := range items {
		if it.table {
			if m := tierTable.FindStringSubmatch(it.key); m != nil && validRank(m[1]) {
				tier(m[1])
			} else if !slices.Contains(tables, it.key) {
				return fail(it.line, "unknown table %s", it.key)
			}
			continue
		}
		v := doc.Values[it.key]
		str := func() (string, error) {
			if v.Kind != toml.String {
				return "", &toml.Error{File: name, Line: it.line, Msg: it.key + " must be a string"}
			}
			return v.Str, nil
		}
		oneOf := func(allowed []string) (string, error) {
			s, err := str()
			if err == nil && !slices.Contains(allowed, s) {
				err = &toml.Error{File: name, Line: it.line, Msg: it.key + " must be " + orList(allowed)}
			}
			return s, err
		}
		var err error
		switch m := tierKey.FindStringSubmatch(it.key); {
		case it.key == "profile":
			c.Profile, err = str()
		case it.key == "testing.level":
			c.Testing, err = oneOf(Levels)
		case m != nil && m[2] == "effort":
			tier(m[1]).Effort, err = oneOf(efforts)
		case m != nil && m[2] == "name":
			tier(m[1]).Name, err = str()
		case m != nil:
			tier(m[1]).Model, err = str()
		case slices.Contains(counts, it.key):
			if v.Kind != toml.Int {
				return fail(it.line, "%s must be an integer", it.key)
			}
			if v.Int < 1 {
				return fail(it.line, "%s must be at least 1", it.key)
			}
			n := int(v.Int)
			switch it.key {
			case "models.reviewer":
				c.Reviewer = n
			case "models.max_tier":
				c.MaxTier = n
			case "escalation.top_attempts":
				c.TopAttempts = n
			case "parallel.devs":
				c.Devs = n
			}
		default:
			return fail(it.line, "unknown key %s", it.key)
		}
		if err != nil {
			return Config{}, err
		}
	}

	if _, ok := doc.Values["profile"]; !ok {
		return fail(0, "profile is missing")
	}
	if len(tiers) == 0 {
		return fail(0, "no tiers")
	}
	for _, t := range tiers {
		c.Tiers = append(c.Tiers, *t)
	}
	slices.SortFunc(c.Tiers, func(a, b Tier) int { return cmp.Compare(a.Rank, b.Rank) })
	for _, t := range c.Tiers {
		if t.Effort == "" {
			key := fmt.Sprintf("tiers.%d", t.Rank)
			return fail(doc.TableLines[key], "%s.effort is missing", key)
		}
	}
	highest := c.Tiers[len(c.Tiers)-1].Rank
	if c.MaxTier == 0 {
		c.MaxTier = highest
	}
	if c.Reviewer == 0 {
		c.Reviewer = min(2, highest)
	}
	return c, nil
}

func validRank(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func orList(s []string) string {
	return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
}

// defaultModels are the models crew init writes for each rank, by harness.
// A harness that is not listed gets empty models, which mean the session's
// model.
var defaultModels = map[string][3]string{
	"claude-code": {"haiku", "sonnet", "opus"},
}

// Template returns the configuration crew init writes for profile, with the
// default models for harness.
func Template(profile, harness string) []byte {
	m := defaultModels[harness]
	return fmt.Appendf(nil, `# Skeleton Crew configuration.
profile = %q

# Ranks run from 1 upwards with no gaps. An empty model means the session's model.
[tiers.1]
name   = "light"
model  = %q
effort = "medium"

[tiers.2]
name   = "standard"
model  = %q
effort = "medium"

[tiers.3]
name   = "heavy"
model  = %q
effort = "high"

[models]
reviewer = 2   # rank the Reviewer always uses
max_tier = 3   # escalation stops here

[escalation]
top_attempts = 2   # attempts at max_tier before the ticket comes to you

[testing]
level = "standard"   # minimal | standard | thorough

[parallel]
devs = 3   # Devs that work at once
`, profile, m[0], m[1], m[2])
}

// DetectHarness names the agent harness crew runs in, from its environment,
// or returns "" when it cannot tell.
func DetectHarness(getenv func(string) string) string {
	if a := getenv("AI_AGENT"); a != "" {
		name, _, _ := strings.Cut(a, "_")
		return name
	}
	switch {
	case getenv("CLAUDECODE") == "1":
		return "claude-code"
	case getenv("OPENCODE") == "1":
		return "opencode"
	}
	return ""
}
