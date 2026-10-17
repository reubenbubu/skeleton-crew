package cli

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/gate"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// keptLogs is how many logs .crew/logs/ keeps.
const keptLogs = 50

func runTicketSubmit(args []string, env Env) int {
	fs := flag.NewFlagSet("ticket submit", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
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
	issue, err := ticket.InProgress(c, h.number)
	if err != nil {
		return finish(env, err, false)
	}
	repo := &git.Repo{Dir: h.dir}
	sha, base, err := pushedCommit(repo, h.branch)
	if err != nil {
		return finish(env, err, false)
	}
	flagged, err := gateCommit(env, r, repo, c, h.number, issue.Body, prof, base, sha)
	if err != nil || flagged == nil {
		return finish(env, err, flagged == nil)
	}
	if err := ticket.Submit(c, h.number); err != nil {
		return finish(env, err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int      `json:"number"`
			Commit string   `json:"commit"`
			Stage  string   `json:"stage"`
			Flags  []string `json:"flags"`
		}{h.number, sha, "4:waiting-review", flagged})
	} else {
		err = lines(env.Stdout, append(flagged, "gate passed "+sha[:7], fmt.Sprintf("#%d stage=4:waiting-review", h.number))...)
	}
	return finish(env, err, false)
}

// pushedCommit returns the commit the worktree's branch has pushed, which
// must be HEAD with nothing uncommitted, and where the branch left main.
func pushedCommit(repo *git.Repo, branch string) (sha, base string, err error) {
	clean, err := repo.Clean()
	if err != nil {
		return "", "", err
	}
	if !clean {
		return "", "", errors.New("uncommitted changes; crew commit them first")
	}
	head, err := repo.Head()
	if err != nil {
		return "", "", err
	}
	if base, err = repo.MergeBase("main"); err != nil {
		return "", "", err
	}
	if head == base {
		return "", "", errors.New("no commits on " + branch)
	}
	// MergeBase has fetched, so an error here means the branch is not on
	// origin.
	if pushed, err := repo.Branch(branch); err != nil || pushed != head {
		return "", "", errors.New("the last commit is not pushed to origin/" + branch)
	}
	return head, base, nil
}

// gateCommit runs the gate on commit sha of ticket number and posts its
// crew/gate status. It returns the gate's flags, never nil, when the gate
// passed. When it failed, it prints why and returns nil.
func gateCommit(env Env, root string, repo *git.Repo, c *github.Client, number int, body string, prof profile.Profile, base, sha string) ([]string, error) {
	if err := setGate(c, sha, "pending", "gate running"); err != nil {
		return nil, err
	}
	res, err := runGate(env, root, repo, c, number, body, prof, base, sha)
	if err != nil {
		setGate(c, sha, "error", "gate could not run")
		return nil, err
	}
	if res.failed != "" {
		statusErr := setGate(c, sha, "failure", "gate failed: "+res.failed)
		ls := append(res.flags, res.problems...)
		if res.output != "" {
			ls = append(ls, strings.TrimSuffix(res.output, "\n"))
		}
		last := "gate failed: " + res.failed
		if res.log != "" {
			last += "; full output: " + res.log
		}
		lines(env.Stderr, append(ls, last)...)
		return nil, statusErr
	}
	if err := setGate(c, sha, "success", "gate passed"); err != nil {
		return nil, err
	}
	return append([]string{}, res.flags...), nil
}

// gated reports whether commit sha has passed the gate.
func gated(c *github.Client, sha string) (bool, error) {
	s, err := c.CombinedStatus(sha)
	return slices.ContainsFunc(s.Statuses, func(st github.Status) bool {
		return st.Context == "crew/gate" && st.State == "success"
	}), err
}

func setGate(c *github.Client, sha, state, description string) error {
	return c.CreateStatus(sha, github.Status{State: state, Context: "crew/gate", Description: description})
}

// gateResult is the outcome of a gate run. failed is "" when it passed,
// and otherwise names what failed, such as "tdd" or "test: exit 1".
type gateResult struct {
	failed   string
	flags    []string
	problems []string // from the checks that need no containers
	output   string   // the end of the failing step's output
	log      string   // the full output, relative to the main checkout
}

// runGate runs the gate on commit sha of ticket number, whose branch left
// main at base, in a detached worktree of the main checkout at root.
func runGate(env Env, root string, repo *git.Repo, c *github.Client, number int, body string, prof profile.Profile, base, sha string) (gateResult, error) {
	var res gateResult
	dir := filepath.Join(root, ".crew", "worktrees", "gate", strconv.Itoa(number))
	main := &git.Repo{Dir: root}
	if err := main.AddDetached(dir, sha); err != nil {
		return res, err
	}
	defer main.RemoveWorktree(dir)

	// The checks that need no containers come first, so they fail fast.
	files, err := repo.Files(sha)
	if err != nil {
		return res, err
	}
	tests := map[string]string{}
	for _, f := range files {
		if prof.IsTest(f) {
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
			if err != nil {
				return res, err
			}
			tests[f] = string(b)
		}
	}
	named := gate.Named(tests)
	var failed []string
	subjects, err := repo.Subjects(base, sha)
	if err != nil {
		return res, err
	}
	if gate.Behaviour(subjects) {
		red, err := ticket.Red(c, number)
		if err != nil {
			return res, err
		}
		criteria := slices.SortedFunc(maps.Keys(ticket.Criteria(number, body)), gate.CompareIDs)
		if p := gate.Untested(criteria, red, named); len(p) > 0 {
			res.problems, failed = append(res.problems, p...), append(failed, "tdd")
		}
	}
	if len(tests) > 0 && len(prof.Skip) > 0 {
		diff, err := repo.Diff(base, sha)
		if err != nil {
			return res, err
		}
		if p := gate.Skipped(diff, prof.Skip, prof.IsTest); len(p) > 0 {
			res.problems, failed = append(res.problems, p...), append(failed, "skipped")
		}
	}
	known, err := ticket.Index(c)
	if err != nil {
		return res, err
	}
	if res.flags, err = gate.Stale(named, known, func(f string) (t time.Time, err error) { return repo.LastChange(sha, f) }); err != nil {
		return res, err
	}
	if len(failed) > 0 {
		res.failed = strings.Join(failed, ", ")
		return res, nil
	}

	err = containerSteps(env, root, dir, compose.GateProject(root, number), "gate-"+strconv.Itoa(number), prof, &res)
	return res, err
}

// containerSteps runs the gate's container steps in the worktree at dir,
// in compose project, and logs them to a file in .crew/logs/ whose name
// ends with name. It records the log, and any failure, in res.
func containerSteps(env Env, root, dir, project, name string, prof profile.Profile, res *gateResult) error {
	res.log = filepath.ToSlash(filepath.Join(".crew", "logs", env.Clock.Now().UTC().Format("20060102T150405Z")+"-"+name+".log"))
	logs := filepath.Join(root, ".crew", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(root, filepath.FromSlash(res.log)))
	if err != nil {
		return err
	}
	f, err := gate.Run{
		Compose: env.Compose, Clock: env.Clock, Poll: env.HealthPoll, Timeout: env.HealthTimeout,
		Dir: dir, Project: project, Commands: prof.Commands, Log: log,
	}.Steps()
	// Log housekeeping never decides the gate's result.
	log.Close()
	prune(logs)
	if err != nil {
		return err
	}
	if f != nil {
		res.failed, res.output = f.Reason, tail(f.Output, 20)
	}
	return nil
}

var logName = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-.+\.log$`)

// prune deletes all but the newest logs in dir. Their names start with
// the time, so they sort by age.
func prune(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var logs []string
	for _, e := range entries {
		if !e.IsDir() && logName.MatchString(e.Name()) {
			logs = append(logs, e.Name())
		}
	}
	slices.Sort(logs)
	for _, name := range logs[:max(0, len(logs)-keptLogs)] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
