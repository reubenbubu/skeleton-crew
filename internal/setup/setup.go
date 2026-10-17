// Package setup sets up a project for Skeleton Crew: the repository, the
// labels, the crew files and the application files. It is what crew init
// does.
package setup

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
)

// CommitMessage is the message of the commit that adds the files.
const CommitMessage = "Set up Skeleton Crew"

// ignored are the lines crew adds to .gitignore.
var ignored = []string{".crew/worktrees/", ".crew/logs/", ".crew/lead.lock*", ".crew/events.*", ".crew/ports.toml", ".crew/main.off"}

type Options struct {
	Dir     string
	Profile profile.Profile
	Harness string // for the default models; "" when unknown
	Install string // how the user installs crew; "" for release
	Repo    string // owner/name of the repository to create; "" for the default
	Public  bool
	GH      github.Runner
}

type Result struct {
	Repo       string   `json:"repo"`
	Created    bool     `json:"created"`
	Visibility string   `json:"visibility,omitempty"` // private or public, when created
	Labels     []string `json:"labels"`               // the labels created
	Wrote      []string `json:"wrote"`                // the files written, relative to Dir
	Added      []string `json:"added"`                // the custom profile's directory, when its changes were committed
	Commit     string   `json:"commit"`               // "" when nothing was committed
}

// RepoName checks that s has the form owner/name.
var RepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Init sets up the project in o.Dir. It checks everything it can before it
// changes anything, and stops at the first problem.
func Init(o Options) (Result, error) {
	res := Result{Labels: []string{}, Wrote: []string{}, Added: []string{}}
	dir := o.Dir
	r := &git.Repo{Dir: dir}

	top, err := git.Toplevel(dir)
	if err != nil {
		return res, err
	}
	if top != "" && !sameDir(top, dir) {
		return res, fmt.Errorf("not at the root of the git repository: %s", top)
	}
	var origin string
	if top != "" {
		branch, err := r.CurrentBranch()
		if err != nil {
			return res, err
		}
		if branch != "main" {
			return res, fmt.Errorf("checked out branch is %s, not main", branch)
		}
		if origin, err = r.Origin(); err != nil {
			return res, err
		}
		if origin != "" {
			repo, ok := github.RepoFromURL(origin)
			if !ok {
				return res, fmt.Errorf("origin is not on github.com: %s", origin)
			}
			res.Repo = repo
		}
		changed, err := r.Changed(".gitignore")
		if err != nil {
			return res, err
		}
		if changed {
			return res, errors.New(".gitignore has uncommitted changes")
		}
	}

	cfgData, err := os.ReadFile(filepath.Join(dir, config.Path))
	haveCfg := err == nil
	if errors.Is(err, fs.ErrNotExist) {
		install := cmp.Or(o.Install, "release")
		cfgData = config.Template(o.Profile.Name, o.Harness, install)
	} else if err != nil {
		return res, err
	}
	cfg, err := config.Parse(config.Path, cfgData)
	if err != nil {
		return res, err
	}
	if cfg.Profile != o.Profile.Name {
		return res, fmt.Errorf("%s has profile %s, not %s", config.Path, cfg.Profile, o.Profile.Name)
	}
	isNew, err := newProject(dir)
	if err != nil {
		return res, err
	}

	remoteMain := false
	if origin != "" {
		remoteMain, err = r.HasBranch("main")
		if err != nil {
			return res, err
		}
		if remoteMain {
			if _, err := r.SyncMain(); err != nil {
				return res, err
			}
		}
	} else if err := createRepo(o, r, top != "", &res); err != nil {
		return res, err
	}

	if res.Labels, err = createLabels(github.New(res.Repo, o.GH), cfg); err != nil {
		return res, err
	}

	w := writer{dir: dir, res: &res}
	if !haveCfg {
		w.file(config.Path, cfgData)
	}
	w.tree(o.Profile.Docs, ".crew/docs")
	var ignore []string
	if isNew && o.Profile.Scaffold != nil {
		w.tree(o.Profile.Scaffold, ".")
		if b, err := fs.ReadFile(o.Profile.Scaffold, ".gitignore"); err == nil {
			ignore = strings.Fields(string(b))
		}
	}
	w.ignore(append(ignore, ignored...))
	if w.err != nil {
		return res, w.err
	}
	slices.Sort(res.Wrote)
	paths := res.Wrote
	if c := o.Profile.Custom; c != "" {
		changed, err := r.Changed(c)
		if err != nil {
			return res, err
		}
		if changed {
			res.Added = append(res.Added, c)
			paths = append(slices.Clone(paths), c)
		}
	}

	if len(paths) == 0 {
		// A run whose push failed left main unpushed; finish it.
		if res.Commit, err = r.LocalMain(); err != nil || res.Commit == "" || remoteMain {
			res.Commit = ""
			return res, err
		}
		return res, r.PushMain()
	}
	if res.Commit, err = r.CommitOnly(CommitMessage, paths); err != nil {
		return res, err
	}
	return res, r.PushMain()
}

// createRepo creates the GitHub repository and adds it as origin, after
// running git init if there is no repository yet.
func createRepo(o Options, r *git.Repo, haveGit bool, res *Result) error {
	c := github.New("", o.GH)
	u, err := c.User()
	if err != nil {
		return err
	}
	owner, name := u.Login, filepath.Base(o.Dir)
	if o.Repo != "" {
		owner, name, _ = strings.Cut(o.Repo, "/")
	}
	org := ""
	if !strings.EqualFold(owner, u.Login) {
		org = owner
	}
	protocol, err := github.GitProtocol(o.GH)
	if err != nil {
		return err
	}
	repo, err := c.CreateRepo(org, name, !o.Public)
	if err != nil {
		return err
	}
	res.Repo, res.Created, res.Visibility = repo.FullName, true, "public"
	if repo.Private {
		res.Visibility = "private"
	}
	if !haveGit {
		if err := git.Init(o.Dir); err != nil {
			return err
		}
	}
	url := repo.CloneURL
	if protocol == "ssh" {
		url = repo.SSHURL
	}
	return r.AddOrigin(url)
}

// createLabels creates the labels the repository lacks and returns their
// names. A label that exists is left as it is.
func createLabels(c *github.Client, cfg config.Config) ([]string, error) {
	existing, err := c.Labels()
	if err != nil {
		return nil, err
	}
	var ranks []int
	for _, t := range cfg.Tiers {
		ranks = append(ranks, t.Rank)
	}
	created := []string{}
	for _, l := range labels.All(ranks) {
		// GitHub matches label names without regard to case.
		if slices.ContainsFunc(existing, func(e github.Label) bool { return strings.EqualFold(e.Name, l.Name) }) {
			continue
		}
		if err := c.CreateLabel(l); err != nil {
			return nil, err
		}
		created = append(created, l.Name)
	}
	return created, nil
}

// newProject reports whether dir holds nothing besides .git, AGENTS.md and
// what crew init writes.
func newProject(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !slices.Contains([]string{".git", "AGENTS.md", ".crew", ".gitignore"}, e.Name()) {
			return false, nil
		}
	}
	return true, nil
}

func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// writer writes files that do not exist yet and records them. It never
// overwrites a file. After the first error it does nothing.
type writer struct {
	dir string
	res *Result
	err error
}

func (w *writer) file(rel string, data []byte) {
	if w.err != nil {
		return
	}
	p := filepath.Join(w.dir, filepath.FromSlash(rel))
	if _, err := os.Lstat(p); err == nil {
		return
	} else if !errors.Is(err, fs.ErrNotExist) {
		w.err = err
		return
	}
	if w.err = os.MkdirAll(filepath.Dir(p), 0o755); w.err != nil {
		return
	}
	if w.err = os.WriteFile(p, data, 0o644); w.err == nil {
		w.res.Wrote = append(w.res.Wrote, rel)
	}
}

// tree writes every file of fsys under the directory to, except a
// .gitignore at its root, which ignore merges.
func (w *writer) tree(fsys fs.FS, to string) {
	if fsys == nil {
		return
	}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == ".gitignore" {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		w.file(path.Join(to, p), data)
		return nil
	})
	if w.err == nil {
		w.err = err
	}
}

// ignore adds the lines that .gitignore lacks.
func (w *writer) ignore(lines []string) {
	if w.err != nil {
		return
	}
	p := filepath.Join(w.dir, ".gitignore")
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		w.err = err
		return
	}
	have := map[string]bool{}
	for l := range strings.SplitSeq(string(old), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	b := bytes.Clone(old)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	added := false
	for _, l := range lines {
		if !have[l] {
			b = append(b, l+"\n"...)
			have[l], added = true, true
		}
	}
	if !added {
		return
	}
	if w.err = os.WriteFile(p, b, 0o644); w.err == nil {
		w.res.Wrote = append(w.res.Wrote, ".gitignore")
	}
}
