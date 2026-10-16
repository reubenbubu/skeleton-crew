package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

var ticketCommands = map[string]func([]string, Env) int{
	"new":     runTicketNew,
	"triage":  runTicketTriage,
	"ready":   runTicketReady,
	"show":    runTicketShow,
	"list":    runTicketList,
	"comment": runTicketComment,
	"start":   runTicketStart,
	"ask":     runTicketAsk,
	"stuck":   runTicketStuck,
	"submit":  runTicketSubmit,
	"next":    runTicketNext,
}

func runTicket(args []string, env Env) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return finish(env, usageError{"usage: crew ticket <command>"}, false)
	}
	cmd, ok := ticketCommands[args[0]]
	if !ok {
		return finish(env, usageError{"unknown command: ticket " + args[0]}, false)
	}
	return cmd(args[1:], env)
}

// positional parses args into fs, allowing flags before, between and after
// the positional arguments, which it returns. names are the positional
// arguments the command takes, such as <n>; each must be given and not
// blank, except an optional one such as [<n>], which may be left out.
func positional(fs *flag.FlagSet, args []string, names ...string) ([]string, error) {
	var pos []string
	for {
		if err := parse(fs, args); err != nil {
			return nil, usageError{err.Error()}
		}
		rest := fs.Args()
		if used := len(args) - len(rest); used > 0 && args[used-1] == "--" {
			pos = append(pos, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		pos, args = append(pos, rest[0]), rest[1:]
	}
	if len(pos) > len(names) {
		return nil, usageError{"unexpected argument: " + pos[len(names)]}
	}
	for k, name := range names {
		if k >= len(pos) && strings.HasPrefix(name, "[") {
			break
		}
		if k >= len(pos) || strings.TrimSpace(pos[k]) == "" {
			return nil, usageError{"missing argument: " + name}
		}
	}
	return pos, nil
}

func number(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || strings.HasPrefix(s, "+") {
		return 0, usageError{"invalid ticket number: " + s}
	}
	return n, nil
}

// oneOf checks that the value of flag name is one of values.
func oneOf(name, value string, values []string) error {
	for _, v := range values {
		if v == value {
			return nil
		}
	}
	want := strings.Join(values[:len(values)-1], ", ") + " or " + values[len(values)-1]
	return usageError{fmt.Sprintf("invalid --%s: %s; want %s", name, value, want)}
}

// client returns a client for the GitHub repository that origin points at.
func client(env Env) (*github.Client, error) {
	origin, err := (&git.Repo{Dir: env.Dir}).Origin()
	if err != nil {
		return nil, err
	}
	if origin == "" {
		return nil, errors.New("no origin; run crew init")
	}
	repo, ok := github.RepoFromURL(origin)
	if !ok {
		return nil, fmt.Errorf("origin is not on github.com: %s", origin)
	}
	return github.New(repo, env.GH), nil
}

func lines(w io.Writer, ls ...string) error {
	for _, l := range ls {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}

func runTicketNew(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket new", flag.ContinueOnError)
	priority := fs.String("priority", "medium", "")
	kind := fs.String("kind", "feat", "")
	intake := fs.Bool("intake", false, "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<title>")
	if err == nil {
		err = oneOf("priority", *priority, ticket.Priorities)
	}
	if err == nil {
		err = oneOf("kind", *kind, ticket.Kinds)
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	n, f, err := ticket.Create(c, pos[0], *priority, *kind, *intake)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number   int    `json:"number"`
			Stage    string `json:"stage"`
			Priority string `json:"priority"`
			Kind     string `json:"kind"`
		}{n, f.Stage, f.Priority, f.Kind})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d %s", n, f))
	}
	return finish(env, err, false)
}

func runTicketTriage(args []string, env Env) int {
	return runMove("triage", "2:drafting", ticket.Triage, args, env)
}

func runTicketReady(args []string, env Env) int {
	return runMove("ready", "3:eligible", ticket.Ready, args, env)
}

// runMove runs a command that moves ticket <n> to stage to.
func runMove(name, to string, move func(*github.Client, int) error, args []string, env Env) int {
	fs := flag.NewFlagSet("ticket "+name, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, c, err := ticketArgs(fs, args, env)
	if err == nil {
		err = move(c, n)
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			Stage  string `json:"stage"`
		}{n, to})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d stage=%s", n, to))
	}
	return finish(env, err, false)
}

// ticketArgs parses the arguments of a command that takes only <n> and
// returns the ticket number and a client.
func ticketArgs(fs *flag.FlagSet, args []string, env Env) (int, *github.Client, error) {
	pos, err := positional(fs, args, "<n>")
	if err != nil {
		return 0, nil, err
	}
	n, err := number(pos[0])
	if err != nil {
		return 0, nil, err
	}
	c, err := client(env)
	return n, c, err
}

func runTicketShow(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket show", flag.ContinueOnError)
	all := fs.Bool("all", false, "")
	asJSON := fs.Bool("json", false, "")
	n, c, err := ticketArgs(fs, args, env)
	if err != nil {
		return finish(env, err, false)
	}
	v, err := ticket.Show(c, n, *all)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, v)
	} else {
		_, err = io.WriteString(env.Stdout, render(v))
	}
	return finish(env, err, false)
}

// render formats the terse view of a ticket.
func render(v ticket.View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s\n", v.Number, v.Title)
	if f := v.Fields.String(); f != "" {
		b.WriteString(f + "\n")
	}
	b.WriteString("---\n")
	if v.Body != "" {
		b.WriteString(v.Body)
		if !strings.HasSuffix(v.Body, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("--- timeline\n")
	var entries []string
	for _, e := range v.Timeline {
		s := output.Time(e.At) + " " + e.Event
		if e.Text != "" {
			s += ": " + e.Text
		}
		entries = append(entries, s)
	}
	if len(entries) > 0 {
		b.WriteString(strings.Join(entries, " · ") + "\n")
	}
	b.WriteString("--- comments\n")
	for _, c := range v.Comments {
		who := strings.ToUpper(c.Role)
		if c.Kind == ticket.Human {
			who = c.Author
		}
		fmt.Fprintf(&b, "[%s] %s %s: %s", c.Kind, who, output.Time(c.At), c.Text)
		if !strings.HasSuffix(c.Text, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func runTicketList(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	tickets, err := ticket.List(c)
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

// summaryLines formats tickets one line each, as crew ticket list prints
// them.
func summaryLines(tickets []ticket.Summary) []string {
	var ls []string
	for _, t := range tickets {
		l := fmt.Sprintf("#%d", t.Number)
		if f := t.Fields.String(); f != "" {
			l += " " + f
		}
		ls = append(ls, l+" · "+t.Title)
	}
	return ls
}

func runTicketComment(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket comment", flag.ContinueOnError)
	kind := fs.String("kind", "", "")
	role := fs.String("as", "", "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<n>", "<text>")
	var n int
	if err == nil {
		n, err = number(pos[0])
	}
	switch {
	case err != nil:
	case *kind == "":
		err = usageError{"missing flag: --kind"}
	case *role == "":
		err = usageError{"missing flag: --as"}
	default:
		if err = oneOf("kind", *kind, ticket.CommentKinds); err == nil {
			err = oneOf("as", *role, ticket.Roles)
		}
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err == nil {
		err = ticket.Post(c, n, *kind, *role, pos[1])
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			Kind   string `json:"kind"`
			Role   string `json:"role"`
		}{n, *kind, *role})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d %s posted", n, *kind))
	}
	return finish(env, err, false)
}
