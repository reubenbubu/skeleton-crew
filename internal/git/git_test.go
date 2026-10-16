package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run runs git for test setup, independently of the code under test.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=user", "-c", "user.email=user@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	run(t, dir, "commit", "--allow-empty", "-q", "-m", msg)
	return run(t, dir, "rev-parse", "HEAD")
}

// clones returns an origin with one commit on main, and two clones of it:
// local, the clone under test, and other, which stands in for GitHub
// receiving pushes from elsewhere.
func clones(t *testing.T) (local, other string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	run(t, root, "init", "-q", "--bare", origin)
	local, other = filepath.Join(root, "local"), filepath.Join(root, "other")
	run(t, root, "clone", "-q", origin, other)
	commit(t, other, "first")
	run(t, other, "push", "-q", "origin", "main")
	run(t, root, "clone", "-q", origin, local)
	return local, other
}

// #3-AC5
func TestBranchFetchesFirst(t *testing.T) {
	local, other := clones(t)
	run(t, other, "switch", "-q", "-c", "feat/1/x")
	want := commit(t, other, "work")
	run(t, other, "push", "-q", "origin", "feat/1/x")

	got, err := (&Repo{Dir: local}).Branch("feat/1/x")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

// #3-AC5
func TestSyncMainFetchesFirst(t *testing.T) {
	local, other := clones(t)
	want := commit(t, other, "second")
	run(t, other, "push", "-q", "origin", "main")

	got, err := (&Repo{Dir: local}).SyncMain()
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

// #3-AC6
func TestSyncMainFastForwardsCheckedOutMain(t *testing.T) {
	local, other := clones(t)
	commit(t, other, "second")
	os.WriteFile(filepath.Join(other, "file"), []byte("x"), 0o644)
	run(t, other, "add", "file")
	want := commit(t, other, "third")
	run(t, other, "push", "-q", "origin", "main")

	if _, err := (&Repo{Dir: local}).SyncMain(); err != nil {
		t.Fatal(err)
	}
	if got := run(t, local, "rev-parse", "main"); got != want {
		t.Fatalf("main at %s, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(local, "file")); err != nil {
		t.Fatalf("working tree not updated: %v", err)
	}
}

// #3-AC6: main checked out in another worktree is updated there, so that
// checkout stays clean.
func TestSyncMainFastForwardsMainInAnotherWorktree(t *testing.T) {
	local, other := clones(t)
	wt := filepath.Join(filepath.Dir(local), "wt")
	run(t, local, "worktree", "add", "-q", "-b", "feat/1/x", wt)
	os.WriteFile(filepath.Join(other, "file"), []byte("x"), 0o644)
	run(t, other, "add", "file")
	want := commit(t, other, "second")
	run(t, other, "push", "-q", "origin", "main")

	if _, err := (&Repo{Dir: wt}).SyncMain(); err != nil {
		t.Fatal(err)
	}
	if got := run(t, local, "rev-parse", "main"); got != want {
		t.Fatalf("main at %s, want %s", got, want)
	}
	if s := run(t, local, "status", "--porcelain"); s != "" {
		t.Fatalf("main checkout not clean:\n%s", s)
	}
}

// #3-AC6
func TestSyncMainFastForwardsMainNotCheckedOut(t *testing.T) {
	local, other := clones(t)
	run(t, local, "switch", "-q", "-c", "feat/1/x")
	want := commit(t, other, "second")
	run(t, other, "push", "-q", "origin", "main")

	if _, err := (&Repo{Dir: local}).SyncMain(); err != nil {
		t.Fatal(err)
	}
	if got := run(t, local, "rev-parse", "main"); got != want {
		t.Fatalf("main at %s, want %s", got, want)
	}
}

// #3-AC6
func TestSyncMainRefusesDivergedMain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote bool
	}{{"ahead", false}, {"diverged", true}} {
		t.Run(tc.name, func(t *testing.T) {
			local, other := clones(t)
			commit(t, local, "local one")
			before := commit(t, local, "local two")
			if tc.remote {
				commit(t, other, "remote")
				run(t, other, "push", "-q", "origin", "main")
			}
			_, err := (&Repo{Dir: local}).SyncMain()
			var d *DivergedError
			if !errors.As(err, &d) || d.Ahead != 2 {
				t.Fatalf("got %v, want a DivergedError with 2 commits", err)
			}
			if want := "local main has 2 commits that origin/main does not have"; err.Error() != want {
				t.Fatalf("message %q, want %q", err.Error(), want)
			}
			if got := run(t, local, "rev-parse", "main"); got != before {
				t.Fatalf("main moved to %s", got)
			}
		})
	}
}

// #3-AC3
func TestRealRemoteIsRefusedInTests(t *testing.T) {
	local, _ := clones(t)
	run(t, local, "remote", "set-url", "origin", "https://github.com/reubenbubu/skeleton-crew.git")
	_, err := (&Repo{Dir: local}).Branch("main")
	if err == nil || !strings.Contains(err.Error(), "transport 'https' not allowed") {
		t.Fatalf("got %v, want the https transport refused", err)
	}
	if strings.Contains(err.Error(), "\n") || !strings.HasPrefix(err.Error(), "git failed: git fetch") {
		t.Fatalf("not one terse line: %q", err.Error())
	}
}

// #3-AC6
func TestDivergedMessageSingular(t *testing.T) {
	if got, want := (&DivergedError{Ahead: 1}).Error(), "local main has 1 commit that origin/main does not have"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// #3-AC5
func TestMissingBranchSaysSo(t *testing.T) {
	local, _ := clones(t)
	_, err := (&Repo{Dir: local}).Branch("feat/9/none")
	if want := "git failed: origin/feat/9/none does not exist"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

// #3-AC6: a fast-forward blocked in another worktree names that worktree.
func TestBlockedFastForwardNamesWorktree(t *testing.T) {
	local, other := clones(t)
	wt := filepath.Join(filepath.Dir(local), "wt")
	run(t, local, "worktree", "add", "-q", "-b", "feat/1/x", wt)
	os.WriteFile(filepath.Join(other, "file"), []byte("theirs"), 0o644)
	run(t, other, "add", "file")
	commit(t, other, "second")
	run(t, other, "push", "-q", "origin", "main")
	os.WriteFile(filepath.Join(local, "file"), []byte("mine"), 0o644)

	_, err := (&Repo{Dir: wt}).SyncMain()
	if err == nil || !strings.HasPrefix(err.Error(), "git failed: git -C "+local+" merge --ff-only") {
		t.Fatalf("got %v, want the failure to name %s", err, local)
	}
}
