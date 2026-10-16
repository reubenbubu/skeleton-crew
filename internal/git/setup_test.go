package git

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// identity gives commits made by the code under test an author.
func identity(t *testing.T) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = user\n\temail = user@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
}

// #4-AC1
func TestInitCreatesMain(t *testing.T) {
	dir := t.TempDir()
	if top, err := Toplevel(dir); err != nil || top != "" {
		t.Fatalf("before init: %q %v", top, err)
	}
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if top, err := Toplevel(dir); err != nil || top != dir {
		t.Fatalf("after init: %q %v", top, err)
	}
	r := &Repo{Dir: dir}
	if b, err := r.CurrentBranch(); err != nil || b != "main" {
		t.Fatalf("branch %q %v", b, err)
	}
}

// #4-AC2
func TestToplevelOfSubdirectory(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	if top, err := Toplevel(sub); err != nil || top != dir {
		t.Fatalf("got %q %v", top, err)
	}
}

// #4-AC1 #4-AC2
func TestOrigin(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	r := &Repo{Dir: dir}
	if u, err := r.Origin(); err != nil || u != "" {
		t.Fatalf("no origin: %q %v", u, err)
	}
	if err := r.AddOrigin("https://github.com/user/app.git"); err != nil {
		t.Fatal(err)
	}
	// The URL as configured, not as rewritten by url.<base>.insteadOf.
	run(t, dir, "config", "url./elsewhere/.insteadOf", "https://github.com/")
	if u, err := r.Origin(); err != nil || u != "https://github.com/user/app.git" {
		t.Fatalf("got %q %v", u, err)
	}
}

// #4-AC2
func TestCurrentBranchDetached(t *testing.T) {
	local, _ := clones(t)
	run(t, local, "checkout", "-q", "--detach")
	if _, err := (&Repo{Dir: local}).CurrentBranch(); err == nil || err.Error() != "git failed: HEAD is detached" {
		t.Fatalf("got %v", err)
	}
}

// #4-AC7
func TestChanged(t *testing.T) {
	local, _ := clones(t)
	r := &Repo{Dir: local}
	os.WriteFile(filepath.Join(local, ".gitignore"), []byte("x\n"), 0o644)
	if ch, err := r.Changed(".gitignore"); err != nil || !ch {
		t.Fatalf("untracked: %v %v", ch, err)
	}
	run(t, local, "add", ".gitignore")
	run(t, local, "commit", "-q", "-m", "ignore")
	if ch, err := r.Changed(".gitignore"); err != nil || ch {
		t.Fatalf("committed: %v %v", ch, err)
	}
	if ch, err := r.Changed("missing"); err != nil || ch {
		t.Fatalf("missing: %v %v", ch, err)
	}
}

// #4-AC7
func TestCommitOnlyLeavesOtherChangesAlone(t *testing.T) {
	identity(t)
	local, _ := clones(t)
	r := &Repo{Dir: local}
	write := func(name, body string) {
		p := filepath.Join(local, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	write("mine.txt", "user's work\n")
	write("staged.txt", "user's staged work\n")
	run(t, local, "add", "staged.txt")
	write(".crew/crew.toml", "profile = \"python\"\n")
	write("Containerfile", "FROM x\n")

	sha, err := r.CommitOnly("Set up Skeleton Crew", []string{".crew/crew.toml", "Containerfile"})
	if err != nil {
		t.Fatal(err)
	}
	if head := run(t, local, "rev-parse", "HEAD"); head != sha {
		t.Fatalf("returned %s, HEAD is %s", sha, head)
	}
	files := strings.Fields(run(t, local, "show", "--name-only", "--format=", "HEAD"))
	if !slices.Equal(files, []string{".crew/crew.toml", "Containerfile"}) {
		t.Fatalf("committed %v", files)
	}
	if msg := run(t, local, "log", "-1", "--format=%s"); msg != "Set up Skeleton Crew" {
		t.Fatalf("message %q", msg)
	}
	if st := run(t, local, "status", "--porcelain"); st != "A  staged.txt\n?? mine.txt" {
		t.Fatalf("status %q", st)
	}
}

// #4-AC1 #4-AC7
func TestPushMain(t *testing.T) {
	identity(t)
	local, other := clones(t)
	r := &Repo{Dir: local}
	os.WriteFile(filepath.Join(local, "f"), []byte("x\n"), 0o644)
	sha, err := r.CommitOnly("Set up Skeleton Crew", []string{"f"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PushMain(); err != nil {
		t.Fatal(err)
	}
	if got, err := (&Repo{Dir: other}).Branch("main"); err != nil || got != sha {
		t.Fatalf("origin/main %s %v, want %s", got, err, sha)
	}
}

// #4-AC1
func TestPushMainToEmptyOrigin(t *testing.T) {
	identity(t)
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	run(t, root, "init", "-q", "--bare", origin)
	dir := filepath.Join(root, "app")
	os.Mkdir(dir, 0o755)
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	r := &Repo{Dir: dir}
	r.AddOrigin(origin)
	if has, err := r.HasBranch("main"); err != nil || has {
		t.Fatalf("empty origin: %v %v", has, err)
	}
	os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644)
	sha, err := r.CommitOnly("Set up Skeleton Crew", []string{"f"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PushMain(); err != nil {
		t.Fatal(err)
	}
	if got := run(t, origin, "rev-parse", "main"); got != sha {
		t.Fatalf("origin main %s, want %s", got, sha)
	}
	if up := run(t, dir, "rev-parse", "--abbrev-ref", "main@{upstream}"); up != "origin/main" {
		t.Fatalf("upstream %q", up)
	}
}

// #4-AC1
func TestHasBranch(t *testing.T) {
	local, _ := clones(t)
	r := &Repo{Dir: local}
	if has, err := r.HasBranch("main"); err != nil || !has {
		t.Fatalf("main: %v %v", has, err)
	}
	if has, err := r.HasBranch("nope"); err != nil || has {
		t.Fatalf("nope: %v %v", has, err)
	}
	var e *Error
	broken := &Repo{Dir: t.TempDir()}
	if _, err := broken.HasBranch("main"); !errors.As(err, &e) {
		t.Fatalf("not a repo: %v", err)
	}
}
