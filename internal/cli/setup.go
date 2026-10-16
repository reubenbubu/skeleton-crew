package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/doctor"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
	"github.com/reubenbubu/skeleton-crew/internal/setup"
)

var commands = map[string]func([]string, Env) int{
	"init":   runInit,
	"doctor": runDoctor,
	"ticket": runTicket,
	"dm":     runDM,
	"commit": runCommit,
	"env":    runEnv,
	"run":    runRun,
	"review": runReview,
	"brief":  runBrief,
	"merge":  runMerge,
	"status": runStatus,
	"events": runEvents,
}

// usageError is a problem with the command line: exit 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// finish prints err, if any, and returns the exit code: 2 for a usage
// error, 1 for any other failure, and otherwise 1 if failed is true.
func finish(env Env, err error, failed bool) int {
	var u usageError
	switch {
	case errors.As(err, &u):
		fmt.Fprintln(env.Stderr, err)
		return 2
	case err != nil:
		fmt.Fprintln(env.Stderr, err)
		return 1
	case failed:
		return 1
	}
	return 0
}

// flags parses args into fs, which must take no positional arguments.
func flags(fs *flag.FlagSet, args []string) error {
	if err := parse(fs, args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() > 0 {
		return usageError{"unexpected argument: " + fs.Arg(0)}
	}
	return nil
}

func runInit(args []string, env Env) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	name := fs.String("profile", "", "")
	harness := fs.String("harness", "", "")
	repo := fs.String("repo", "", "")
	public := fs.Bool("public", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	if *name == "" {
		return finish(env, usageError{"missing flag: --profile"}, false)
	}
	p, err := profile.Load(env.Dir, *name)
	if errors.Is(err, profile.ErrUnknown) {
		return finish(env, usageError{err.Error()}, false)
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *repo != "" && !setup.RepoName.MatchString(*repo) {
		return finish(env, usageError{"invalid --repo: " + *repo + "; want <owner>/<name>"}, false)
	}
	if *harness == "" {
		*harness = config.DetectHarness(env.Getenv)
	}
	res, err := setup.Init(setup.Options{Dir: env.Dir, Profile: p, Harness: *harness, Repo: *repo, Public: *public, GH: env.GH})
	if err != nil {
		return finish(env, err, false)
	}
	checks := diagnose(env, *harness)
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			setup.Result
			Checks []doctor.Check `json:"checks"`
		}{res, checks})
	} else {
		err = printInit(env.Stdout, res, checks)
	}
	return finish(env, err, doctor.Failed(checks))
}

func printInit(w io.Writer, res setup.Result, checks []doctor.Check) error {
	line := "repo " + res.Repo
	if res.Created {
		line += " created " + res.Visibility
	}
	lines := []string{line, fmt.Sprintf("labels created %d", len(res.Labels))}
	for _, f := range res.Wrote {
		lines = append(lines, "wrote "+f)
	}
	for _, f := range res.Added {
		lines = append(lines, "added "+f)
	}
	if res.Commit == "" {
		lines = append(lines, "commit none")
	} else {
		lines = append(lines, "commit "+res.Commit[:min(7, len(res.Commit))]+" pushed main")
	}
	for _, c := range checks {
		lines = append(lines, c.String())
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}
