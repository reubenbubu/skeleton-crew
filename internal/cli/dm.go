package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/lead"
	"github.com/reubenbubu/skeleton-crew/internal/oplock"
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
//
// The first check in a run first takes the operation lock, waiting while
// another lead-only command holds it, so one lead-only command at a time
// acts in the checkout. A later check, before a step that changes state,
// stops a command whose lead was taken over meanwhile.
func checkLead(env Env, token string) error {
	r, err := root(env)
	if err != nil {
		return err
	}
	if err := checkToken(env, r, token); err != nil || env.op == nil || env.op.lock != nil {
		return err
	}
	// Checked again once the lock is held: the lead may have been taken
	// over while this command waited.
	if env.op.lock, err = takeOperation(env, r); err != nil {
		return err
	}
	return checkToken(env, r, token)
}

// checkToken refuses with not-lead unless token is the lead's, without
// taking the operation lock.
func checkToken(env Env, r, token string) error {
	warning, err := lead.Check(r, token)
	warn(env, warning)
	return err
}

// takeOperation takes the operation lock in the main checkout at r,
// trying every env.LockPoll while another command holds it.
func takeOperation(env Env, r string) (*oplock.Lock, error) {
	path := filepath.Join(r, OperationPath)
	holder := fmt.Sprintf("crew %s pid %d since %s", env.op.what, os.Getpid(), output.Time(env.Clock.Now()))
	said := false
	for {
		l, ok, err := oplock.TryLock(path, holder)
		if err != nil || ok {
			return l, err
		}
		if !said {
			h := oplock.Holder(path)
			if h == "" {
				h = "another lead-only command"
			}
			fmt.Fprintf(env.Stderr, "waiting for %s to finish\n", h)
			said = true
		}
		<-env.Clock.After(env.LockPoll)
	}
}

// OperationPath is the operation lock, relative to the root of the main
// checkout.
const OperationPath = ".crew/lead.lock.op"

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
