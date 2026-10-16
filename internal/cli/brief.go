package cli

import (
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/brief"
	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// knowledge are the knowledge files every briefing holds in full.
var knowledge = []string{".crew/docs/product.md", ".crew/docs/architecture.md", ".crew/docs/conventions.md"}

// doc is one knowledge file at origin/main.
type doc struct {
	Path    string `json:"path"`
	Text    string `json:"text"`
	Missing bool   `json:"missing"`
}

// briefing is what crew brief prints. Tier, Model and Effort are set for a
// Dev with a ticket and for a Reviewer; an empty Model means the session's
// model.
type briefing struct {
	Role    string       `json:"role"`
	Ticket  int          `json:"ticket,omitempty"`
	Tier    int          `json:"tier,omitempty"`
	Model   string       `json:"model,omitempty"`
	Effort  string       `json:"effort,omitempty"`
	Testing string       `json:"testing"`
	Harness string       `json:"harness,omitempty"` // for the DM: the harness it runs in, or unknown
	Wiring  string       `json:"wiring,omitempty"`  // for the DM: the harness's recorded wiring, or none
	Rules   string       `json:"rules"`
	View    *ticket.View `json:"view,omitempty"`
	Docs    []doc        `json:"docs"`
	ADRs    []brief.ADR  `json:"adrs"`
}

func runBrief(args []string, env Env) int {
	fs := flag.NewFlagSet("brief", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<role>", "[<ticket>]")
	if err != nil {
		return finish(env, err, false)
	}
	b := briefing{Role: pos[0]}
	rules, ok := brief.Rules(b.Role)
	if !ok {
		return finish(env, usageError{"unknown role: " + b.Role + "; want pm, dm, dev or reviewer"}, false)
	}
	b.Rules = rules
	if len(pos) > 1 {
		if b.Ticket, err = number(pos[1]); err != nil {
			return finish(env, err, false)
		}
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	cfg, _, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	b.Testing = cfg.Testing
	if b.Role == "dm" {
		b.Harness = config.DetectHarness(env.Getenv)
		if !harnessName.MatchString(b.Harness) {
			b.Harness = "unknown"
		} else if b.Wiring = cfg.Harnesses[b.Harness].Wiring; b.Wiring == "" {
			b.Wiring = "none"
		}
	}
	if b.Ticket != 0 {
		if err := b.fromTicket(env, cfg); err != nil {
			return finish(env, err, false)
		}
	}
	if b.Role == "reviewer" {
		if err := b.tier(cfg, cfg.Reviewer, fmt.Sprintf("models.reviewer is %d", cfg.Reviewer)); err != nil {
			return finish(env, err, false)
		}
	}
	if err := b.readKnowledge(r); err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, b)
	} else {
		_, err = fmt.Fprint(env.Stdout, b.String())
	}
	return finish(env, err, false)
}

// fromTicket adds the ticket, its testing level and, for a Dev, its tier.
func (b *briefing) fromTicket(env Env, cfg config.Config) error {
	c, err := client(env)
	if err != nil {
		return err
	}
	v, err := ticket.Show(c, b.Ticket, false)
	if err != nil {
		return err
	}
	b.View = &v
	if t := v.Fields.Testing; t != "" {
		switch {
		case strings.Contains(t, ","):
			return fmt.Errorf("#%d has more than one testing level: %s", b.Ticket, t)
		case !slices.Contains(config.Levels, t):
			return fmt.Errorf("#%d has testing:%s; want minimal, standard or thorough", b.Ticket, t)
		}
		b.Testing = t
	}
	if b.Role != "dev" {
		return nil
	}
	switch t := v.Fields.Tier; {
	case t == "":
		return fmt.Errorf("#%d has no tier; crew ticket start sets it", b.Ticket)
	case strings.Contains(t, ","):
		return fmt.Errorf("#%d has more than one tier: %s", b.Ticket, t)
	default:
		rank, _ := strconv.Atoi(t)
		return b.tier(cfg, rank, fmt.Sprintf("#%d has tier:%s", b.Ticket, t))
	}
}

// tier sets the model and effort of rank, or refuses with what, such as
// "#12 has tier:5", when the configuration does not define it.
func (b *briefing) tier(cfg config.Config, rank int, what string) error {
	for _, t := range cfg.Tiers {
		if t.Rank == rank {
			b.Tier, b.Model, b.Effort = rank, t.Model, t.Effort
			return nil
		}
	}
	return fmt.Errorf("%s, which %s does not define", what, config.Path)
}

// readKnowledge reads the knowledge files and the ADRs at origin/main.
func (b *briefing) readKnowledge(root string) error {
	repo := &git.Repo{Dir: root}
	sha, err := repo.Branch("main")
	if err != nil {
		return err
	}
	for _, p := range knowledge {
		text, ok, err := repo.Read(sha, p)
		if err != nil {
			return err
		}
		b.Docs = append(b.Docs, doc{Path: p, Text: text, Missing: !ok})
	}
	files, err := repo.Files(sha)
	if err != nil {
		return err
	}
	b.ADRs = []brief.ADR{}
	for _, f := range files {
		if !strings.HasPrefix(f, brief.ADRDir+"/") {
			continue
		}
		text, _, err := repo.Read(sha, f)
		if err != nil {
			return err
		}
		if a, ok := brief.ParseADR(f, text); ok {
			b.ADRs = append(b.ADRs, a)
		}
	}
	return nil
}

// String formats the briefing: a header, then one section per part, each
// starting with a line "=== <name>".
func (b *briefing) String() string {
	var s strings.Builder
	s.WriteString("brief " + b.Role)
	if b.Ticket != 0 {
		fmt.Fprintf(&s, " #%d", b.Ticket)
	}
	s.WriteString("\n")
	var fields []string
	if b.Tier != 0 {
		fields = append(fields, fmt.Sprintf("tier=%d", b.Tier))
		if b.Model != "" {
			fields = append(fields, "model="+b.Model)
		}
		fields = append(fields, "effort="+b.Effort)
	}
	fields = append(fields, "testing="+b.Testing)
	if b.Harness != "" {
		fields = append(fields, "harness="+b.Harness)
	}
	if b.Wiring != "" {
		fields = append(fields, "wiring="+b.Wiring)
	}
	s.WriteString(strings.Join(fields, " ") + "\n")
	s.WriteString("=== rules\n" + withNewline(b.Rules))
	if b.View != nil {
		s.WriteString("=== ticket\n" + render(*b.View))
	}
	for _, d := range b.Docs {
		if d.Missing {
			s.WriteString("=== " + d.Path + ": missing\n")
		} else {
			s.WriteString("=== " + d.Path + "\n" + withNewline(d.Text))
		}
	}
	s.WriteString("=== " + brief.ADRDir + "\n")
	for _, a := range b.ADRs {
		s.WriteString(a.Line() + "\n")
	}
	return s.String()
}

func withNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
