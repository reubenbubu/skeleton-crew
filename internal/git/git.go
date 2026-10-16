// Package git runs git for crew. Every read of a branch fetches from origin
// first, and local main only ever fast-forwards to origin/main.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Repo is a local clone. Use one Repo per command, so each command fetches
// once before it reads a branch. A command that polls a branch makes a new
// Repo for each poll.
type Repo struct {
	Dir     string
	fetched bool
}

// Error is a git command that failed. Its message is one terse line.
type Error struct {
	Args   []string
	Detail string
}

func (e *Error) Error() string {
	if len(e.Args) == 0 {
		return "git failed: " + e.Detail
	}
	return fmt.Sprintf("git failed: git %s: %s", strings.Join(e.Args, " "), e.Detail)
}

// DivergedError means local main has commits that origin/main does not.
type DivergedError struct {
	Ahead int
}

func (e *DivergedError) Error() string {
	noun := "commits"
	if e.Ahead == 1 {
		noun = "commit"
	}
	return fmt.Sprintf("local main has %d %s that origin/main does not have", e.Ahead, noun)
}

// git runs git in dir and returns its trimmed output.
//
// In a test binary it allows only local remotes, so an automated test can
// never reach a real GitHub host.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	if testing.Testing() {
		cmd.Env = append(cmd.Env, "GIT_ALLOW_PROTOCOL=file")
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(errOut.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", &Error{Args: args, Detail: strings.Join(strings.Fields(reason(detail)), " ")}
	}
	return strings.TrimSpace(out.String()), nil
}

// reason picks the line of git's stderr that says why it failed: the first
// fatal or error line, or else the last line.
func reason(stderr string) string {
	lines := strings.Split(stderr, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "fatal: ") || strings.HasPrefix(l, "error: ") {
			return l
		}
	}
	return lines[len(lines)-1]
}

func (r *Repo) fetch() error {
	if r.fetched {
		return nil
	}
	if _, err := git(r.Dir, "fetch", "--quiet", "--prune", "origin"); err != nil {
		return err
	}
	r.fetched = true
	return nil
}

// Branch fetches from origin, unless this Repo already has, and returns the
// commit that origin/<name> points to.
func (r *Repo) Branch(name string) (string, error) {
	if err := r.fetch(); err != nil {
		return "", err
	}
	sha, err := git(r.Dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+name+"^{commit}")
	if err != nil {
		return "", &Error{Detail: "origin/" + name + " does not exist"}
	}
	return sha, nil
}

// SyncMain fetches from origin, unless this Repo already has, fast-forwards
// local main to origin/main and returns that commit. It refuses with a
// DivergedError if local main has commits that origin/main does not.
func (r *Repo) SyncMain() (string, error) {
	remote, err := r.Branch("main")
	if err != nil {
		return "", err
	}
	local, err := git(r.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/main^{commit}")
	if err != nil {
		return "", &Error{Detail: "local main does not exist"}
	}
	if local == remote {
		return remote, nil
	}
	counts, err := git(r.Dir, "rev-list", "--count", "refs/remotes/origin/main..refs/heads/main")
	if err != nil {
		return "", err
	}
	ahead, err := strconv.Atoi(counts)
	if err != nil {
		return "", &Error{Args: []string{"rev-list", "--count"}, Detail: "unexpected output " + strconv.Quote(counts)}
	}
	if ahead > 0 {
		return "", &DivergedError{Ahead: ahead}
	}
	// Move main where it is checked out, so that working tree follows it.
	dir, err := r.mainWorktree()
	if err != nil {
		return "", err
	}
	if dir != "" {
		if _, err = git(dir, "merge", "--ff-only", "--quiet", "refs/remotes/origin/main"); err != nil {
			e := err.(*Error)
			e.Args = append([]string{"-C", dir}, e.Args...)
		}
	} else {
		_, err = git(r.Dir, "update-ref", "refs/heads/main", remote, local)
	}
	if err != nil {
		return "", err
	}
	return remote, nil
}

// mainWorktree returns the worktree that has main checked out, or "".
func (r *Repo) mainWorktree() (string, error) {
	out, err := git(r.Dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	var dir string
	for line := range strings.SplitSeq(out, "\n") {
		if d, ok := strings.CutPrefix(line, "worktree "); ok {
			dir = d
		} else if line == "branch refs/heads/main" {
			return dir, nil
		}
	}
	return "", nil
}

// Toplevel returns the root of the git repository that contains dir, or ""
// when dir is in no repository.
func Toplevel(dir string) (string, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			return "", nil
		}
		return "", err
	}
	return top, nil
}

// Init makes dir a new repository whose branch is main.
func Init(dir string) error {
	_, err := git(dir, "init", "--quiet", "--initial-branch=main")
	return err
}

// Origin returns the URL of origin as configured, or "" when there is no
// origin.
func (r *Repo) Origin() (string, error) {
	url, err := git(r.Dir, "config", "--get", "remote.origin.url")
	// git config exits 1, saying nothing, when the key is not set.
	if e, ok := err.(*Error); ok && e.Detail == "exit status 1" {
		return "", nil
	}
	return url, err
}

func (r *Repo) AddOrigin(url string) error {
	_, err := git(r.Dir, "remote", "add", "origin", url)
	return err
}

// CurrentBranch returns the branch that is checked out, even one with no
// commits yet.
func (r *Repo) CurrentBranch() (string, error) {
	b, err := git(r.Dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", &Error{Detail: "HEAD is detached"}
	}
	return b, nil
}

// Changed reports whether path has changes that are not committed,
// including when it is untracked.
func (r *Repo) Changed(path string) (bool, error) {
	out, err := git(r.Dir, "status", "--porcelain", "--untracked-files=all", "--", path)
	return out != "", err
}

// CommitOnly commits paths, and nothing else, to the current branch and
// returns the new commit. Other staged and unstaged changes stay as they
// are.
func (r *Repo) CommitOnly(msg string, paths []string) (string, error) {
	if _, err := git(r.Dir, append([]string{"add", "--"}, paths...)...); err != nil {
		return "", err
	}
	if _, err := git(r.Dir, append([]string{"commit", "--quiet", "--only", "-m", msg, "--"}, paths...)...); err != nil {
		return "", err
	}
	return git(r.Dir, "rev-parse", "HEAD")
}

// LocalMain returns the commit local main points to, or "" when main has no
// commits yet.
func (r *Repo) LocalMain() (string, error) {
	sha, err := git(r.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/main^{commit}")
	if err != nil {
		return "", nil
	}
	return sha, nil
}

// PushMain pushes local main to origin and makes origin/main its upstream.
func (r *Repo) PushMain() error {
	_, err := git(r.Dir, "push", "--quiet", "--set-upstream", "origin", "refs/heads/main:refs/heads/main")
	return err
}

// HasBranch fetches from origin, unless this Repo already has, and reports
// whether origin has branch name.
func (r *Repo) HasBranch(name string) (bool, error) {
	if err := r.fetch(); err != nil {
		return false, err
	}
	_, err := git(r.Dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+name+"^{commit}")
	return err == nil, nil
}

// MainRoot returns the root of the main checkout of the repository that
// contains dir, even from a linked worktree, or "" when dir is in no
// repository.
func MainRoot(dir string) (string, error) {
	if top, err := Toplevel(dir); top == "" || err != nil {
		return "", err
	}
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	// The main worktree is always listed first.
	first, _, _ := strings.Cut(out, "\n")
	return strings.TrimPrefix(first, "worktree "), nil
}

// AddWorktree fetches from origin, unless this Repo already has, and checks
// out branch in a new worktree at path. A new branch starts at origin/main.
// A worktree of branch that is already at path is kept as it is.
func (r *Repo) AddWorktree(path, branch string) error {
	base, err := r.Branch("main")
	if err != nil {
		return err
	}
	out, err := git(r.Dir, "worktree", "list", "--porcelain")
	if err != nil {
		return err
	}
	var dir string
	for line := range strings.SplitSeq(out, "\n") {
		if d, ok := strings.CutPrefix(line, "worktree "); ok {
			dir = d
		} else if line == "branch refs/heads/"+branch && filepath.Clean(dir) == filepath.Clean(path) {
			return nil
		}
	}
	if _, err := git(r.Dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = git(r.Dir, "worktree", "add", "--quiet", path, branch)
		return err
	}
	_, err = git(r.Dir, "worktree", "add", "--quiet", "-b", branch, path, base)
	return err
}

// Change is one path in a diff, with its status: A added, M modified, D
// deleted, or T type changed.
type Change struct {
	Status, Path string
}

// StageAll stages every change in the working tree, including untracked
// files.
func (r *Repo) StageAll() error {
	_, err := git(r.Dir, "add", "--all")
	return err
}

// Unstage resets the index to HEAD and leaves the working tree as it is.
func (r *Repo) Unstage() error {
	_, err := git(r.Dir, "reset", "--quiet")
	return err
}

// Staged lists the staged changes, in path order.
func (r *Repo) Staged() ([]Change, error) {
	return r.changes("diff", "--cached", "--name-status", "--no-renames", "-z")
}

// ChangedSince lists the paths that the index changes from commit base,
// in path order.
func (r *Repo) ChangedSince(base string) ([]Change, error) {
	return r.changes("diff", "--cached", "--name-status", "--no-renames", "-z", base)
}

func (r *Repo) changes(args ...string) ([]Change, error) {
	out, err := git(r.Dir, args...)
	if err != nil || out == "" {
		return nil, err
	}
	f := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var cs []Change
	for k := 0; k+1 < len(f); k += 2 {
		cs = append(cs, Change{Status: f[k], Path: f[k+1]})
	}
	return cs, nil
}

// StagedDiff returns the staged diff of paths, without context lines.
func (r *Repo) StagedDiff(paths ...string) (string, error) {
	return git(r.Dir, append([]string{"--literal-pathspecs", "diff", "--cached", "--no-renames", "--no-color", "-U0", "--"}, paths...)...)
}

// MergeBase fetches from origin, unless this Repo already has, and returns
// the commit where HEAD left origin/<branch>.
func (r *Repo) MergeBase(branch string) (string, error) {
	if _, err := r.Branch(branch); err != nil {
		return "", err
	}
	return git(r.Dir, "merge-base", "HEAD", "refs/remotes/origin/"+branch)
}

// Commit commits the index to the current branch and returns the new
// commit.
func (r *Repo) Commit(msg string) (string, error) {
	if _, err := git(r.Dir, "commit", "--quiet", "-m", msg); err != nil {
		return "", err
	}
	return git(r.Dir, "rev-parse", "HEAD")
}

// PushBranch pushes local branch name to origin and makes it the upstream.
func (r *Repo) PushBranch(name string) error {
	_, err := git(r.Dir, "push", "--quiet", "--set-upstream", "origin", "refs/heads/"+name+":refs/heads/"+name)
	return err
}
