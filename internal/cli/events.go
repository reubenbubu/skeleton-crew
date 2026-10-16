package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/events"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

func runEvents(args []string, env Env) int {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	token := leadFlag(fs)
	wait := fs.Bool("wait", false, "")
	test := fs.Bool("test", false, "")
	asJSON := fs.Bool("json", false, "")
	err := flags(fs, args)
	switch {
	case err != nil:
	case *test && !*wait:
		err = usageError{"--test needs --wait"}
	case *wait && *asJSON:
		err = usageError{"--json does not go with --wait"}
	}
	if err == nil {
		err = checkLead(env, *token)
	}
	if err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	if *wait {
		return finish(env, waitForEvents(env, r, *test), false)
	}

	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	now := env.Clock.Now().UTC().Truncate(time.Second)
	since, ok, warning := events.Read(r)
	warn(env, warning)
	evs := []ticket.Event{}
	if ok {
		if evs, err = ticket.Events(c, since, now); err != nil {
			return finish(env, err, false)
		}
		evs = append([]ticket.Event{}, evs...)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Events []ticket.Event `json:"events"`
		}{evs})
	} else {
		ls := []string{"no events"}
		if len(evs) > 0 {
			ls = nil
		}
		for _, e := range evs {
			ls = append(ls, fmt.Sprintf("%s #%d %s", output.Time(e.At), e.Number, e.What))
		}
		err = lines(env.Stdout, ls...)
	}
	// Marked read only once printed, so a failure repeats events rather
	// than losing them.
	if err == nil {
		err = events.MarkRead(r, now)
	}
	return finish(env, err, false)
}

// waitForEvents blocks until something is unread, without marking it read,
// and returns quietly once a newer wait replaces it. With test, it returns
// after env.EventsTest instead, as if an event had arrived.
func waitForEvents(env Env, root string, test bool) error {
	// A wait that cannot run must not replace one that can.
	c, err := client(env)
	if err != nil {
		return err
	}
	name, err := events.Begin(root)
	if err != nil {
		return err
	}
	if test {
		<-env.Clock.After(env.EventsTest)
		return nil
	}
	for events.Current(root, name) {
		since, ok, warning := events.Read(root)
		warn(env, warning)
		if !ok {
			// Nothing records what was read, so everything is unread.
			return nil
		}
		evs, err := ticket.Events(c, since, env.Clock.Now())
		if err != nil || len(evs) > 0 {
			// Replaced meanwhile, it leaves the waking to the newer wait.
			if !events.Current(root, name) {
				return nil
			}
			return err
		}
		<-env.Clock.After(env.EventsPoll)
	}
	return nil
}

var harnessName = regexp.MustCompile(`^` + config.HarnessName + `$`)

func runWiring(args []string, env Env) int {
	fs := flag.NewFlagSet("dm wiring", flag.ContinueOnError)
	token := leadFlag(fs)
	wait := fs.Bool("wait", false, "")
	own := fs.String("own", "", "")
	var files repeated
	fs.Var(&files, "file", "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<harness>")
	switch {
	case err != nil:
	case *wait == (*own != ""):
		err = usageError{"want --wait or --own"}
	case len(files) > 0 && *own == "":
		err = usageError{"--file needs --own"}
	case !harnessName.MatchString(pos[0]):
		err = usageError{"invalid harness: " + pos[0] + "; want letters, digits, - and _"}
	case strings.ContainsFunc(*own, unicode.IsControl):
		err = usageError{"invalid --own: control characters"}
	}
	for _, f := range files {
		if err == nil && !filepath.IsLocal(f) {
			err = usageError{"invalid --file: " + f + "; want a path inside the project"}
		}
	}
	if err == nil {
		err = checkLead(env, *token)
	}
	if err != nil {
		return finish(env, err, false)
	}
	name := pos[0]
	h := config.Harness{Wiring: "wait"}
	if *own != "" {
		h = config.Harness{Wiring: "own", Start: *own}
		for _, f := range files {
			if f = filepath.ToSlash(filepath.Clean(f)); !slices.Contains(h.Files, f) {
				h.Files = append(h.Files, f)
			}
		}
	}
	if err := recordWiring(env, name, h); err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, map[string]string{"harness": name, "wiring": h.Wiring})
	} else {
		err = lines(env.Stdout, "recorded "+name+" wiring="+h.Wiring)
	}
	return finish(env, err, false)
}

// recordWiring records h for harness name in the configuration, and commits
// it with the wiring's files straight to main, like crew init. A failure
// changes nothing.
func recordWiring(env Env, name string, h config.Harness) error {
	r, err := root(env)
	if err != nil {
		return err
	}
	for _, f := range h.Files {
		if _, err := os.Stat(filepath.Join(r, f)); err != nil {
			return errors.New("no such file: " + f)
		}
	}
	repo := &git.Repo{Dir: r}
	if branch, err := repo.CurrentBranch(); err != nil {
		return err
	} else if branch != "main" {
		return fmt.Errorf("checked out branch is %s, not main", branch)
	}
	if changed, err := repo.Changed(config.Path); err != nil {
		return err
	} else if changed {
		return errors.New(config.Path + " has uncommitted changes")
	}
	head, err := repo.SyncMain()
	if err != nil {
		return err
	}
	path := filepath.Join(r, config.Path)
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	b := config.Record(old, name, h)
	if _, err := config.Parse(config.Path, b); err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	paths := append([]string{config.Path}, h.Files...)
	var changed []string
	for _, p := range paths {
		c, err := repo.Changed(p)
		if err != nil {
			return errors.Join(err, os.WriteFile(path, old, 0o644))
		}
		if c && !slices.Contains(changed, p) {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	// Undo puts main, the index of the committed paths and the
	// configuration back, and says so if it cannot.
	undo := func(err error) error {
		if rerr := errors.Join(repo.ResetTo(head, changed), os.WriteFile(path, old, 0o644)); rerr != nil {
			return fmt.Errorf("%w; undoing it failed too: %v", err, rerr)
		}
		return err
	}
	if _, err := repo.CommitOnly("Record the "+name+" wiring", changed); err != nil {
		return undo(err)
	}
	if err := repo.PushMain(); err != nil {
		return undo(err)
	}
	return nil
}
