package cli

import (
	"flag"
	"fmt"

	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

func runStatus(args []string, env Env) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	role := fs.String("as", "", "")
	asJSON := fs.Bool("json", false, "")
	_, err := positional(fs, args)
	if err == nil && *role != "" {
		err = oneOf("as", *role, []string{"pm", "dm"})
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	s, err := ticket.ReadStatus(c, *role, env.Clock.Now())
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, s)
	} else {
		err = lines(env.Stdout, statusLines(s)...)
	}
	return finish(env, err, false)
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
