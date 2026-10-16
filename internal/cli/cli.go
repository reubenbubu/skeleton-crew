// Package cli parses the command line and dispatches commands.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/doctor"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/output"
)

// Env is everything a command may use from the outside world, so tests can
// replace it.
type Env struct {
	Version        string
	Stdout, Stderr io.Writer
	Clock          clock.Clock
	Dir            string                  // the working directory
	Getenv         func(key string) string // reads the environment
	GH             github.Runner           // runs gh
	Probe          doctor.Probe            // runs local tools for checks
	Compose        compose.Runner          // runs the container engine's compose
}

// Run executes crew with args, which exclude the program name, and returns
// the exit code.
func Run(args []string, env Env) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, ok := commands[args[0]]
		if !ok {
			fmt.Fprintf(env.Stderr, "unknown command: %s\n", args[0])
			return 2
		}
		return cmd(args[1:], env)
	}
	fs := flag.NewFlagSet("crew", flag.ContinueOnError)
	version := fs.Bool("version", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := parse(fs, args); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "unknown command: %s\n", fs.Arg(0))
		return 2
	}
	if *version {
		var err error
		if *asJSON {
			err = output.JSON(env.Stdout, map[string]string{"version": env.Version})
		} else {
			_, err = fmt.Fprintln(env.Stdout, env.Version)
		}
		if err != nil {
			fmt.Fprintln(env.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(env.Stderr, "usage: crew <command>")
	return 2
}

// parse parses args into fs and turns the flag package's errors into one
// terse line that names the argument as it was given.
func parse(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	if err == nil {
		return nil
	}
	var name string
	if errors.Is(err, flag.ErrHelp) {
		name = "help"
	} else if n, ok := strings.CutPrefix(err.Error(), "flag provided but not defined: -"); ok {
		name = n
	} else {
		return errors.New(err.Error())
	}
	for _, a := range args {
		if a == "--" {
			break
		}
		n, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if strings.HasPrefix(a, "-") && (n == name || (name == "help" && n == "h")) {
			return fmt.Errorf("unknown flag: %s", a)
		}
	}
	return fmt.Errorf("unknown flag: -%s", name)
}
