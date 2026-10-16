package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// leadTicketArgs parses the arguments of a lead-only command that takes
// <n> and the positional arguments names, checks the lead's token, and
// returns the ticket number and the other positional arguments.
func leadTicketArgs(fs *flag.FlagSet, args []string, env Env, names ...string) (int, []string, error) {
	token := leadFlag(fs)
	pos, err := positional(fs, args, append([]string{"<n>"}, names...)...)
	if err != nil {
		return 0, nil, err
	}
	n, err := number(pos[0])
	if err != nil {
		return 0, nil, err
	}
	return n, pos[1:], checkLead(env, *token)
}

// stageLine prints that ticket n is at stage, terse or as JSON.
func stageLine(env Env, asJSON bool, n int, stage string) error {
	if asJSON {
		return output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			Stage  string `json:"stage"`
		}{n, stage})
	}
	return lines(env.Stdout, fmt.Sprintf("#%d stage=%s", n, stage))
}

func runTicketRetry(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket retry", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, _, err := leadTicketArgs(fs, args, env)
	if err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	cfg, _, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	rank, err := ticket.Retry(c, n, cfg.MaxTier, cfg.TopAttempts)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			Stage  string `json:"stage"`
			Tier   int    `json:"tier"`
		}{n, "4:in-progress", rank})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d stage=4:in-progress tier=%d", n, rank))
	}
	return finish(env, err, false)
}

func runTicketAppeal(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket appeal", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, pos, err := leadTicketArgs(fs, args, env, "<reason>")
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err == nil {
		err = ticket.Appeal(c, n, pos[0])
	}
	if err == nil {
		err = stageLine(env, *asJSON, n, "4:waiting-review")
	}
	return finish(env, err, false)
}

func runTicketSplit(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket split", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, titles, err := leadTicketArgs(fs, args, env, "<title>...")
	if err == nil && len(titles) < 2 {
		err = usageError{"split needs at least two titles"}
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	children, err := ticket.Split(c, n, titles)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number   int              `json:"number"`
			Stage    string           `json:"stage"`
			Children []ticket.Summary `json:"children"`
		}{n, ticket.Cancelled, children})
	} else {
		var ls []string
		for _, ch := range children {
			ls = append(ls, fmt.Sprintf("#%d %s", ch.Number, ch.Fields))
		}
		err = lines(env.Stdout, append(ls, fmt.Sprintf("#%d stage=%s", n, ticket.Cancelled))...)
	}
	return finish(env, err, false)
}

func runTicketCancel(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket cancel", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, _, err := leadTicketArgs(fs, args, env)
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err == nil {
		err = ticket.Cancel(c, n)
	}
	if err == nil {
		err = stageLine(env, *asJSON, n, ticket.Cancelled)
	}
	return finish(env, err, false)
}

// repeated is a flag that may be given more than once.
type repeated []string

func (r *repeated) String() string { return fmt.Sprint(*r) }

func (r *repeated) Set(s string) error {
	*r = append(*r, s)
	return nil
}

// ticketNumbers parses each of ss as a ticket number.
func ticketNumbers(ss []string) ([]int, error) {
	var out []int
	for _, s := range ss {
		n, err := number(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func runTicketEdit(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket edit", flag.ContinueOnError)
	bodyFile := fs.String("body-file", "", "")
	priority := fs.String("priority", "", "")
	kind := fs.String("kind", "", "")
	testing := fs.String("testing", "", "")
	var adds, removes repeated
	fs.Var(&adds, "add-dependency", "")
	fs.Var(&removes, "remove-dependency", "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<n>")
	var n int
	var add, remove []int
	if err == nil {
		n, err = number(pos[0])
	}
	if err == nil {
		add, err = ticketNumbers(adds)
	}
	if err == nil {
		remove, err = ticketNumbers(removes)
	}
	e := ticket.Edit{Priority: *priority, Kind: *kind, Testing: *testing, Add: add, Remove: remove}
	switch {
	case err != nil:
	case *bodyFile == "" && e.Priority == "" && e.Kind == "" && e.Testing == "" && len(add) == 0 && len(remove) == 0:
		err = usageError{"nothing to edit"}
	case e.Priority != "" && oneOf("priority", e.Priority, ticket.Priorities) != nil:
		err = oneOf("priority", e.Priority, ticket.Priorities)
	case e.Kind != "" && oneOf("kind", e.Kind, ticket.Kinds) != nil:
		err = oneOf("kind", e.Kind, ticket.Kinds)
	case e.Testing != "" && oneOf("testing", e.Testing, testingLevels) != nil:
		err = oneOf("testing", e.Testing, testingLevels)
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *bodyFile != "" {
		path := *bodyFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(env.Dir, path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return finish(env, err, false)
		}
		body := string(b)
		e.Body = &body
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	f, revised, err := ticket.Amend(c, n, e, env.Clock.Now())
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		if revised == nil {
			revised = []string{}
		}
		err = output.JSON(env.Stdout, struct {
			Number int `json:"number"`
			ticket.Fields
			Revised []string `json:"revised"`
		}{n, f, revised})
	} else {
		ls := []string{fmt.Sprintf("#%d %s", n, f)}
		for _, id := range revised {
			ls = append(ls, "revised "+id)
		}
		err = lines(env.Stdout, ls...)
	}
	return finish(env, err, false)
}

// testingLevels are the values of crew ticket edit --testing.
var testingLevels = []string{"minimal", "standard", "thorough", "project"}

func runTicketAck(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket ack", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, c, err := ticketArgs(fs, args, env)
	if err == nil {
		err = ticket.Ack(c, n)
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number       int  `json:"number"`
			Acknowledged bool `json:"acknowledged"`
		}{n, true})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d acknowledged", n))
	}
	return finish(env, err, false)
}

// blockedLine prints whether ticket n is blocked, terse or as JSON.
func blockedLine(env Env, asJSON bool, n int, blocked bool) error {
	if asJSON {
		return output.JSON(env.Stdout, struct {
			Number  int  `json:"number"`
			Blocked bool `json:"blocked"`
		}{n, blocked})
	}
	word := "unblocked"
	if blocked {
		word = "blocked"
	}
	return lines(env.Stdout, "#"+strconv.Itoa(n)+" "+word)
}

func runTicketBlock(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket block", flag.ContinueOnError)
	role := fs.String("as", "", "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<n>", "<reason>")
	var n int
	if err == nil {
		n, err = number(pos[0])
	}
	if err == nil && *role == "" {
		err = usageError{"missing flag: --as"}
	}
	if err == nil {
		err = oneOf("as", *role, []string{"pm", "dm"})
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err == nil {
		err = ticket.Block(c, n, *role, pos[1])
	}
	if err == nil {
		err = blockedLine(env, *asJSON, n, true)
	}
	return finish(env, err, false)
}

func runTicketUnblock(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket unblock", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	n, c, err := ticketArgs(fs, args, env)
	if err == nil {
		err = ticket.Unblock(c, n)
	}
	if err == nil {
		err = blockedLine(env, *asJSON, n, false)
	}
	return finish(env, err, false)
}
