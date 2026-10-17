package cli

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/git"

	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

func runStatus(args []string, env Env) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	role := fs.String("as", "", "")
	token := fs.String("lead", "", "")
	asJSON := fs.Bool("json", false, "")
	_, err := positional(fs, args)
	if err == nil && *role != "" {
		err = oneOf("as", *role, []string{"pm", "dm"})
	}
	lead := false
	fs.Visit(func(f *flag.Flag) { lead = lead || f.Name == "lead" })
	if err == nil && lead && *role != "dm" {
		err = usageError{"--lead needs --as dm"}
	}
	if err == nil && lead {
		err = checkLead(env, *token)
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	s, err := ticket.ReadStatus(c, *role, lead, env.Clock.Now())
	if err != nil {
		// Report what was changed before the failure.
		if !*asJSON && len(s.Changes) > 0 {
			lines(env.Stdout, statusLines(ticket.Status{Changes: s.Changes})...)
		}
		return finish(env, err, false)
	}
	// A failed removal stops the removals; the status is still shown.
	var cleanErr error
	if lead {
		cleanErr = checkLead(env, *token)
	}
	if lead && cleanErr == nil {
		var changes []ticket.Change
		changes, cleanErr = removeLeftovers(env, s.Board)
		s.Changes = append(s.Changes, changes...)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, s)
	} else {
		err = lines(env.Stdout, statusLines(s)...)
	}
	if err == nil {
		err = cleanErr
	}
	return finish(env, err, false)
}

// removeLeftovers removes the worktrees under .crew/worktrees, and the
// compose projects that run in them, of every ticket that is not on board
// at a stage from 4 to 6. A ticket's worktrees are its Dev's worktree, in
// compose project crew-<n>, its gate worktree, in crew-<n>-gate, and its
// merge worktree. It returns one change per ticket, and stops at the first
// failure.
func removeLeftovers(env Env, board []ticket.Summary) ([]ticket.Change, error) {
	keep := map[int]bool{}
	for _, t := range board {
		for st := range strings.SplitSeq(t.Fields.Stage, ",") {
			if st >= "4:" && st < "7:" {
				keep[t.Number] = true
			}
		}
	}
	r, err := root(env)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(r, ".crew", "worktrees")
	kinds, err := os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	removed := map[int][]string{}
	var changes []ticket.Change
	done := func() []ticket.Change {
		numbers := slices.Sorted(maps.Keys(removed))
		for _, n := range numbers {
			changes = append(changes, ticket.Change{Number: n, Change: "removed " + strings.Join(removed[n], " ")})
		}
		return changes
	}
	repo := &git.Repo{Dir: r}
	for _, k := range kinds {
		kind := k.Name()
		if !k.IsDir() || (kind != "gate" && kind != "merge" && !slices.Contains(ticket.Kinds, kind)) {
			continue
		}
		numbers, err := os.ReadDir(filepath.Join(base, kind))
		if err != nil {
			return done(), err
		}
		for _, e := range numbers {
			n, err := strconv.Atoi(e.Name())
			if err != nil || n <= 0 || keep[n] || !e.IsDir() {
				continue
			}
			dir := filepath.Join(base, kind, e.Name())
			rel := path.Join(".crew", "worktrees", kind, e.Name())
			switch kind {
			case "gate":
				if err := cleanUp(env, r, compose.GateProject(r, n), dir); err != nil {
					return done(), err
				}
				removed[n] = append(removed[n], rel, compose.GateProject(r, n))
			case "merge":
				if err := repo.RemoveWorktree(dir); err != nil {
					return done(), err
				}
				removed[n] = append(removed[n], rel)
			default:
				slugs, err := os.ReadDir(dir)
				if err != nil {
					return done(), err
				}
				for _, sl := range slugs {
					if !sl.IsDir() {
						continue
					}
					if err := cleanUp(env, r, compose.Project(r, n), filepath.Join(dir, sl.Name())); err != nil {
						return done(), err
					}
					removed[n] = append(removed[n], path.Join(rel, sl.Name()), compose.Project(r, n))
				}
				// An empty directory, such as one crew merge leaves, is
				// removed without a word.
				if err := repo.RemoveWorktree(dir); err != nil {
					return done(), err
				}
			}
		}
	}
	return done(), nil
}

// statusLines formats a status: one section per part, each opened by a line
// "=== <name>", leaving out the empty ones.
func statusLines(s ticket.Status) []string {
	var out []string
	section := func(name string, ls []string) {
		if len(ls) > 0 {
			out = append(append(out, "=== "+name), ls...)
		}
	}
	notes := func(ns []ticket.Note) []string {
		var ls []string
		for _, n := range ns {
			ls = append(ls, fmt.Sprintf("#%d %s: %s", n.Number, output.Time(n.At), n.Text))
		}
		return ls
	}
	var ls []string
	for _, c := range s.Changes {
		ls = append(ls, fmt.Sprintf("#%d %s", c.Number, c.Change))
	}
	section("changes", ls)
	ls = nil
	for _, b := range s.Waiting {
		l := fmt.Sprintf("#%d blocked since %s", b.Number, output.Time(b.Since))
		if b.Question != "" {
			l += ": " + b.Question
		}
		ls = append(ls, l)
	}
	section("waiting for the user", ls)
	section("intake", summaryLines(s.Intake))
	section("drafting", summaryLines(s.Drafting))
	section("decisions", notes(s.Decisions))
	section("dev questions", notes(s.DevQuestions))
	section("board", summaryLines(s.Board))
	ls = nil
	for _, st := range s.Stale {
		ls = append(ls, fmt.Sprintf("#%d %s since %s: %s", st.Number, st.Stage, output.Time(st.Since), st.Flag))
	}
	section("stale", ls)
	ls = nil
	for _, p := range s.Recovery {
		ls = append(ls, fmt.Sprintf("#%d %s: %s", p.Number, p.Stage, p.Action))
	}
	section("recovery", ls)
	if len(out) == 0 {
		out = []string{"nothing needs attention"}
	}
	return out
}

func runTicketNext(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket next", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	tickets, err := ticket.Next(c)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Tickets []ticket.Summary `json:"tickets"`
		}{tickets})
	} else {
		err = lines(env.Stdout, summaryLines(tickets)...)
	}
	return finish(env, err, false)
}
