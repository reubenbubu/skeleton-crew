package cli

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

func runMerge(args []string, env Env) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	token := leadFlag(fs)
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<n>")
	var n int
	if err == nil {
		n, err = number(pos[0])
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
	_, prof, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	issue, f, approved, err := ticket.Approved(c, n)
	if err != nil {
		return finish(env, err, false)
	}
	repo := &git.Repo{Dir: r}
	base, err := repo.SyncMain()
	if err != nil {
		return finish(env, err, false)
	}
	branch := ticket.Branch(f.Kind, n, issue.Title)
	sha, err := repo.Branch(branch)
	if err != nil {
		return finish(env, err, false)
	}
	// A merge that stopped after its rebase was pushed left the branch at
	// a rebase of the approved commit; anything else was never approved.
	if sha != approved {
		same, err := rebaseOf(r, repo, n, approved, sha)
		if err != nil {
			return finish(env, err, false)
		}
		if !same {
			return finish(env, fmt.Errorf("not-approved: %s is at %s, not the approved %s or a rebase of it", branch, sha[:7], approved[:7]), false)
		}
	}
	if err := ticket.PendingMerge(c, n, f.Stage); err != nil {
		return finish(env, err, false)
	}
	var out []string
	// done prints what was done before err, if any, stopped the merge. With
	// --json, a merge that stops prints only the error.
	done := func(err error, failed bool) int {
		if *asJSON {
			return finish(env, err, failed)
		}
		if e := lines(env.Stdout, out...); err == nil {
			err = e
		}
		return finish(env, err, failed)
	}
	upToDate, err := repo.IsAncestor(base, sha)
	if err != nil {
		return done(err, false)
	}
	rebased := !upToDate
	if rebased {
		if sha, err = rebase(r, repo, n, branch, base, sha); err != nil {
			return done(err, false)
		}
		out = append(out, fmt.Sprintf("rebased %s onto main: %s", branch, sha[:7]))
	}
	flagged := []string{}
	passed, err := gated(c, sha)
	if err != nil {
		return done(err, false)
	}
	if !passed {
		if flagged, err = gateCommit(env, r, repo, c, n, issue.Body, prof, base, sha); err != nil || flagged == nil {
			return done(err, flagged == nil)
		}
		out = append(out, append(flagged, "gate passed "+sha[:7])...)
	}
	// Without force, origin takes only a fast-forward of main.
	if err := repo.Push(sha, "main", ""); err != nil {
		return done(err, false)
	}
	// A new Repo fetches again, so local main follows the push.
	if _, err := (&git.Repo{Dir: r}).SyncMain(); err != nil {
		return done(err, false)
	}
	files, err := repo.Files(sha)
	if err != nil {
		return done(err, false)
	}
	deploy := slices.ContainsFunc(files, func(f string) bool { return slices.Contains(ticket.DeployWorkflows, f) })
	stage, err := ticket.Merged(c, n, deploy)
	out = append(out, "merged "+sha[:7]+" into main")
	if err != nil {
		return done(err, false)
	}
	out = append(out, fmt.Sprintf("#%d stage=%s", n, stage))
	wt := path.Join(".crew", "worktrees", branch)
	if err := cleanUp(env, r, compose.Project(r, n), filepath.Join(r, filepath.FromSlash(wt))); err != nil {
		return done(err, false)
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number  int      `json:"number"`
			Commit  string   `json:"commit"`
			Rebased bool     `json:"rebased"`
			Flags   []string `json:"flags"`
			Stage   string   `json:"stage"`
		}{n, sha, rebased, flagged, stage})
		return finish(env, err, false)
	}
	out = append(out, "removed "+wt+" "+compose.Project(r, n))
	return done(nil, false)
}

// rebase rebases commit sha of ticket number's branch onto base, in a
// detached worktree of the main checkout at root, and pushes the result to
// the branch, which must still be at sha. A conflict changes nothing.
func rebase(root string, repo *git.Repo, number int, branch, base, sha string) (string, error) {
	dir := filepath.Join(root, ".crew", "worktrees", "merge", strconv.Itoa(number))
	if err := repo.AddDetached(dir, sha); err != nil {
		return "", err
	}
	defer repo.RemoveWorktree(dir)
	wt := &git.Repo{Dir: dir}
	conflicts, err := wt.Rebase(base)
	if err != nil {
		return "", err
	}
	if len(conflicts) > 0 {
		return "", fmt.Errorf("merge-conflict: #%d stays at 6:pending-merge: %s", number, strings.Join(conflicts, " "))
	}
	head, err := wt.Head()
	if err != nil {
		return "", err
	}
	return head, wt.Push(head, branch, sha)
}

// rebaseOf reports whether commit head is commit approved rebased with its
// content unchanged: rebasing approved onto where head left main gives
// commits with the same trees as head's, one for one. It rebases in a
// detached worktree of the main checkout at root, and a conflict counts as
// a change.
func rebaseOf(root string, repo *git.Repo, number int, approved, head string) (bool, error) {
	onto, err := repo.MergeBaseOf(head, "refs/remotes/origin/main")
	if err != nil {
		return false, err
	}
	want, err := repo.Trees(onto, head)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(root, ".crew", "worktrees", "merge", strconv.Itoa(number))
	if err := repo.AddDetached(dir, approved); err != nil {
		return false, err
	}
	defer repo.RemoveWorktree(dir)
	wt := &git.Repo{Dir: dir}
	if conflicts, err := wt.Rebase(onto); err != nil || len(conflicts) > 0 {
		return false, err
	}
	got, err := wt.Trees(onto, "HEAD")
	return slices.Equal(got, want), err
}

// cleanUp takes down the compose project called project, with its volumes,
// and removes the worktree at wt, where the project runs.
func cleanUp(env Env, root, project, wt string) error {
	dir := wt
	if _, err := os.Stat(wt); err != nil {
		dir = root
	}
	out, code, err := env.Compose(dir, "-p", project, "down", "--volumes", "--remove-orphans", "--rmi", "local")
	if err != nil {
		return err
	}
	if code != 0 {
		env.Stderr.Write([]byte(tail(out, 20)))
		return fmt.Errorf("%s down: exit %d", project, code)
	}
	return (&git.Repo{Dir: root}).RemoveWorktree(wt)
}
