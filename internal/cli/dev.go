package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// loadConfig reads .crew/crew.toml in the main checkout at root, and the
// profile it names.
func loadConfig(root string) (config.Config, profile.Profile, error) {
	b, err := os.ReadFile(filepath.Join(root, config.Path))
	if errors.Is(err, os.ErrNotExist) {
		return config.Config{}, profile.Profile{}, errors.New("no " + config.Path + "; run crew init")
	}
	if err != nil {
		return config.Config{}, profile.Profile{}, err
	}
	cfg, err := config.Parse(config.Path, b)
	if err != nil {
		return cfg, profile.Profile{}, err
	}
	p, err := profile.Load(root, cfg.Profile)
	return cfg, p, err
}

// here is the ticket whose worktree crew runs in.
type here struct {
	dir    string // the root of the worktree
	root   string // the root of the main checkout
	branch string
	kind   string
	number int
}

// ticketHere finds the ticket from the branch checked out where crew runs.
func ticketHere(env Env) (here, error) {
	top, err := git.Toplevel(env.Dir)
	if err != nil {
		return here{}, err
	}
	if top == "" {
		return here{}, errors.New("not in a git repository")
	}
	b, err := (&git.Repo{Dir: top}).CurrentBranch()
	if err != nil {
		return here{}, errors.New("not on a ticket branch: HEAD is detached")
	}
	kind, n, ok := ticket.ParseBranch(b)
	if !ok {
		return here{}, errors.New("not on a ticket branch: " + b)
	}
	r, err := git.MainRoot(top)
	if err != nil {
		return here{}, err
	}
	return here{dir: top, root: r, branch: b, kind: kind, number: n}, nil
}

func runTicketStart(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket start", flag.ContinueOnError)
	token := leadFlag(fs)
	tier := fs.String("tier", "", "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<n>")
	var n int
	if err == nil {
		n, err = number(pos[0])
	}
	if err == nil && *tier == "" {
		err = usageError{"missing flag: --tier"}
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
	cfg, _, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	var ranks []string
	for _, t := range cfg.Tiers {
		ranks = append(ranks, strconv.Itoa(t.Rank))
	}
	if err := oneOf("tier", *tier, ranks); err != nil {
		return finish(env, err, false)
	}
	rank, _ := strconv.Atoi(*tier)
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	i, f, err := ticket.Eligible(c, n)
	if err != nil {
		return finish(env, err, false)
	}
	busy, err := ticket.InFlight(c)
	if err != nil {
		return finish(env, err, false)
	}
	slots := cfg.Devs
	if f.Priority == "critical" {
		slots++
	}
	if len(busy) >= slots {
		var ns []string
		for _, b := range busy {
			ns = append(ns, fmt.Sprintf("#%d", b))
		}
		return finish(env, fmt.Errorf("at-cap: %d of %d slots busy: %s", len(busy), slots, strings.Join(ns, " ")), false)
	}
	branch := ticket.Branch(f.Kind, n, i.Title)
	wt := path.Join(".crew", "worktrees", branch)
	if err := (&git.Repo{Dir: r}).AddWorktree(filepath.Join(r, filepath.FromSlash(wt)), branch); err != nil {
		return finish(env, err, false)
	}
	if err := ticket.Start(c, n, rank); err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number   int    `json:"number"`
			Stage    string `json:"stage"`
			Tier     int    `json:"tier"`
			Branch   string `json:"branch"`
			Worktree string `json:"worktree"`
			Compose  string `json:"compose"`
		}{n, "4:in-progress", rank, branch, wt, compose.Project(r, n)})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d stage=4:in-progress tier=%d branch=%s worktree=%s", n, rank, branch, wt))
	}
	return finish(env, err, false)
}

// tail returns the last n lines of out, each ending in a newline.
func tail(out []byte, n int) string {
	s := strings.TrimRight(string(out), "\n")
	if s == "" {
		return ""
	}
	ls := strings.Split(s, "\n")
	return strings.Join(ls[max(0, len(ls)-n):], "\n") + "\n"
}

var criterionID = regexp.MustCompile(`#[0-9]+-AC[0-9]+`)

// criteria returns the criterion IDs named in the lines that diff, a
// unified diff, adds and removes, each once, in order.
func criteria(diff string) (added, removed []string) {
	for l := range strings.SplitSeq(diff, "\n") {
		var to *[]string
		switch {
		case strings.HasPrefix(l, "+++ b/") || strings.HasPrefix(l, "--- a/") || l == "+++ /dev/null" || l == "--- /dev/null":
			continue
		case strings.HasPrefix(l, "+"):
			to = &added
		case strings.HasPrefix(l, "-"):
			to = &removed
		default:
			continue
		}
		for _, id := range criterionID.FindAllString(l, -1) {
			if !slices.Contains(*to, id) {
				*to = append(*to, id)
			}
		}
	}
	return added, removed
}

func runCommit(args []string, env Env) int {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	red := fs.Bool("red", false, "")
	green := fs.Bool("green", false, "")
	refactor := fs.Bool("refactor", false, "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<message>")
	if err != nil {
		return finish(env, err, false)
	}
	var phase string
	for name, set := range map[string]bool{"red": *red, "green": *green, "refactor": *refactor} {
		if set && phase != "" {
			return finish(env, usageError{"only one of --red, --green and --refactor"}, false)
		}
		if set {
			phase = name
		}
	}
	h, err := ticketHere(env)
	if err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	_, prof, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	if err := ticket.Committable(c, h.number); err != nil {
		return finish(env, err, false)
	}
	repo := &git.Repo{Dir: h.dir}
	rec, err := check(env, repo, h, prof, phase)
	if err != nil {
		return finish(env, err, false)
	}
	msg := fmt.Sprintf("[%s:#%d] ", h.kind, h.number)
	if phase != "" {
		msg += phase + ": "
	}
	msg += pos[0]
	// The red result is recorded before the commit, so no later failure
	// can lose it; the tests failed either way.
	if rec != nil {
		if err := ticket.LogRed(c, h.number, rec.criteria, rec.files, rec.excerpt); err != nil {
			repo.Unstage()
			return finish(env, err, false)
		}
	}
	sha, err := repo.Commit(msg)
	if err != nil {
		repo.Unstage()
		return finish(env, err, false)
	}
	if err := repo.PushBranch(h.branch); err != nil {
		return finish(env, err, false)
	}
	pr, opened, err := ticket.OpenPR(c, h.number, h.kind, h.branch)
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Commit  string `json:"commit"`
			Message string `json:"message"`
			Branch  string `json:"branch"`
			PR      int    `json:"pr"`
			Opened  bool   `json:"opened"`
		}{sha, msg, h.branch, pr, opened})
	} else {
		ls := []string{sha[:min(7, len(sha))] + " " + msg, "pushed " + h.branch}
		if opened {
			ls = append(ls, fmt.Sprintf("opened PR #%d", pr))
		}
		err = lines(env.Stdout, ls...)
	}
	return finish(env, err, false)
}

// redResult is what a red step records on the ticket.
type redResult struct {
	criteria, files []string
	excerpt         string
}

// check stages every change in the worktree and checks the TDD phase, if
// any, by running the tests. When it refuses, it unstages again.
func check(env Env, repo *git.Repo, h here, prof profile.Profile, phase string) (*redResult, error) {
	if err := repo.StageAll(); err != nil {
		return nil, err
	}
	res, err := checkStaged(env, repo, h, prof, phase)
	if err != nil {
		if e := repo.Unstage(); e != nil {
			return nil, e
		}
	}
	return res, err
}

func checkStaged(env Env, repo *git.Repo, h here, prof profile.Profile, phase string) (*redResult, error) {
	staged, err := repo.Staged()
	if err != nil {
		return nil, err
	}
	if len(staged) == 0 {
		return nil, errors.New("nothing to commit")
	}
	var tests, others []string
	for _, ch := range staged {
		switch {
		case !prof.IsTest(ch.Path):
			others = append(others, ch.Path)
		case phase == "refactor" && ch.Status == "A":
			return nil, errors.New("refactor: the commit adds a test file: " + ch.Path)
		default:
			tests = append(tests, ch.Path)
		}
	}
	added, removed := []string(nil), []string(nil)
	if len(tests) > 0 && (phase == "red" || phase == "refactor") {
		diff, err := repo.StagedDiff(tests...)
		if err != nil {
			return nil, err
		}
		added, removed = criteria(diff)
	}
	var run []string
	switch phase {
	case "":
		return nil, nil
	case "red":
		if len(others) > 0 {
			return nil, errors.New("red: the commit changes files that are not tests: " + strings.Join(others, " "))
		}
		for _, ch := range staged {
			if ch.Status != "D" {
				run = append(run, ch.Path)
			}
		}
		if len(run) == 0 {
			return nil, errors.New("red: no changed test file to run")
		}
	default:
		if phase == "refactor" {
			if gained := slices.DeleteFunc(added, func(id string) bool { return slices.Contains(removed, id) }); len(gained) > 0 {
				return nil, errors.New("refactor: the commit adds tests for " + strings.Join(gained, " "))
			}
		}
		base, err := repo.MergeBase("main")
		if err != nil {
			return nil, err
		}
		changed, err := repo.ChangedSince(base)
		if err != nil {
			return nil, err
		}
		for _, ch := range changed {
			if ch.Status != "D" && prof.IsTest(ch.Path) {
				run = append(run, ch.Path)
			}
		}
	}
	out, code, err := compose.Exec(env.Compose, h.dir, h.root, h.number, prof.Commands["test-changed"], run)
	if err != nil {
		return nil, err
	}
	switch {
	case code >= 125 && code <= 127:
		// The engine (125) or the shell (126, 127) failed: no test ran.
		return nil, fmt.Errorf("%s%s: the tests could not run: exit %d", tail(out, 20), phase, code)
	case phase == "red" && code == 0:
		return nil, errors.New("red: the tests pass; a red step needs failing tests")
	case phase == "red":
		return &redResult{criteria: added, files: run, excerpt: tail(out, 20)}, nil
	case code != 0:
		return nil, fmt.Errorf("%s%s: the tests fail", tail(out, 20), phase)
	}
	return nil, nil
}

func runEnv(args []string, env Env) int {
	usage := usageError{"usage: crew env up|down|logs"}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return finish(env, usage, false)
	}
	sub := map[string][]string{"up": {"up", "-d"}, "down": {"down", "--volumes"}, "logs": {"logs", "--no-color"}}
	a, ok := sub[args[0]]
	if !ok {
		return finish(env, usageError{"unknown command: env " + args[0]}, false)
	}
	if err := flags(flag.NewFlagSet("env "+args[0], flag.ContinueOnError), args[1:]); err != nil {
		return finish(env, err, false)
	}
	h, err := ticketHere(env)
	if err != nil {
		return finish(env, err, false)
	}
	project := compose.Project(h.root, h.number)
	out, code, err := env.Compose(h.dir, append([]string{"-p", project}, a...)...)
	switch {
	case err != nil:
	case code != 0:
		env.Stderr.Write([]byte(tail(out, 20)))
		err = fmt.Errorf("env %s: exit %d", args[0], code)
	case args[0] == "logs":
		_, err = env.Stdout.Write(out)
	default:
		err = lines(env.Stdout, project+" "+args[0])
	}
	return finish(env, err, false)
}

func runRun(args []string, env Env) int {
	switch {
	case len(args) == 0:
		return finish(env, usageError{"missing argument: <command>"}, false)
	case strings.HasPrefix(args[0], "-"):
		return finish(env, usageError{"unknown flag: " + args[0]}, false)
	case !slices.Contains(profile.Commands, args[0]):
		return finish(env, usageError{"unknown profile command: " + args[0]}, false)
	}
	h, err := ticketHere(env)
	if err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	_, prof, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	cmd, ok := prof.Commands[args[0]]
	if !ok {
		return finish(env, fmt.Errorf("the %s profile has no %s command", prof.Name, args[0]), false)
	}
	out, code, err := compose.Exec(env.Compose, h.dir, h.root, h.number, cmd, args[1:])
	if err != nil {
		return finish(env, err, false)
	}
	if _, err := env.Stdout.Write(out); err != nil {
		return finish(env, err, false)
	}
	return code
}

func runTicketAsk(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket ask", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<question>")
	if err != nil {
		return finish(env, err, false)
	}
	h, err := ticketHere(env)
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err == nil {
		err = ticket.Post(c, h.number, "question", "dev", pos[0])
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			Kind   string `json:"kind"`
		}{h.number, "question"})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d question posted", h.number))
	}
	return finish(env, err, false)
}

func runTicketStuck(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket stuck", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<reason>")
	if err != nil {
		return finish(env, err, false)
	}
	h, err := ticketHere(env)
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err == nil {
		err = ticket.Stuck(c, h.number, pos[0])
	}
	if err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			Stage  string `json:"stage"`
		}{h.number, "4:review-rejected"})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d stage=4:review-rejected", h.number))
	}
	return finish(env, err, false)
}
