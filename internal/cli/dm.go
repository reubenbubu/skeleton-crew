package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/lead"
	"github.com/reubenbubu/skeleton-crew/internal/output"
)

var dmCommands = map[string]func([]string, Env) int{
	"claim":   runClaim,
	"release": runRelease,
	"wiring":  runWiring,
}

func runDM(args []string, env Env) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return finish(env, usageError{"usage: crew dm <command>"}, false)
	}
	cmd, ok := dmCommands[args[0]]
	if !ok {
		return finish(env, usageError{"unknown command: dm " + args[0]}, false)
	}
	return cmd(args[1:], env)
}

// root returns the root of the main checkout, where the lead lock is.
func root(env Env) (string, error) {
	r, err := git.MainRoot(env.Dir)
	if err == nil && r == "" {
		err = errors.New("not in a git repository")
	}
	return r, err
}

// leadFlag adds --lead to fs, for a lead-only command.
func leadFlag(fs *flag.FlagSet) *string { return fs.String("lead", "", "") }

// checkLead refuses with not-lead unless token is the lead's. It prints a
// warning for an unreadable lock, which counts as no lead.
func checkLead(env Env, token string) error {
	r, err := root(env)
	if err != nil {
		return err
	}
	warning, err := lead.Check(r, token)
	warn(env, warning)
	return err
}

func warn(env Env, warning string) {
	if warning != "" {
		fmt.Fprintln(env.Stderr, warning)
	}
}

func runClaim(args []string, env Env) int {
	fs := flag.NewFlagSet("dm claim", flag.ContinueOnError)
	takeover := fs.Bool("takeover", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	l, warning, err := lead.Claim(r, env.Clock.Now(), *takeover)
	warn(env, warning)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, l)
	} else {
		err = lines(env.Stdout, "lead "+l.Token+" claimed "+output.Time(l.Claimed))
	}
	return finish(env, err, false)
}

func runRelease(args []string, env Env) int {
	fs := flag.NewFlagSet("dm release", flag.ContinueOnError)
	token := leadFlag(fs)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	warning, err := lead.Release(r, *token)
	warn(env, warning)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, map[string]bool{"released": true})
	} else {
		err = lines(env.Stdout, "lead released")
	}
	return finish(env, err, false)
}
