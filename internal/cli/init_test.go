package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/github/fakegithub"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

// project is a directory for crew init, a local bare repository that stands
// in for the GitHub repository's git remote, and the fake GitHub API.
type project struct {
	t        *testing.T
	dir      string
	origin   string
	fullName string // owner/name on GitHub
	fake     *fakegithub.GitHub
	clock    *clock.Fake // the fake GitHub's clock
	env      map[string]string
	compose  compose.Runner
}

// newProject returns an empty directory holding only AGENTS.md, for the
// GitHub repository repo, which does not exist yet. Git rewrites both of
// GitHub's URLs for repo to the local bare repository.
func newProject(t *testing.T, repo string) *project {
	t.Helper()
	root := t.TempDir()
	p := &project{t: t, dir: filepath.Join(root, "app"), origin: filepath.Join(root, "origin.git"), fullName: repo, env: map[string]string{}}
	gitIn(t, root, "init", "-q", "--bare", p.origin)
	if err := os.Mkdir(p.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p.write("AGENTS.md", "# Agent instructions\n")
	cfg := filepath.Join(root, "gitconfig")
	os.WriteFile(cfg, []byte(fmt.Sprintf("[user]\n\tname = user\n\temail = user@example.com\n[init]\n\tdefaultBranch = main\n[url %q]\n\tinsteadOf = https://github.com/%s.git\n\tinsteadOf = git@github.com:%s.git\n", p.origin, repo, repo)), 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	p.clock = clock.NewFake(clock.Epoch)
	p.fake = fakegithub.New(repo, p.clock)
	p.fake.SetExists(false)
	p.compose = (&containers{}).run
	return p
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (p *project) git(args ...string) string { p.t.Helper(); return gitIn(p.t, p.dir, args...) }

func (p *project) write(name, body string) {
	p.t.Helper()
	path := filepath.Join(p.dir, name)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *project) read(name string) string {
	p.t.Helper()
	b, err := os.ReadFile(filepath.Join(p.dir, name))
	if err != nil {
		p.t.Fatal(err)
	}
	return string(b)
}

func (p *project) exists(name string) bool {
	_, err := os.Stat(filepath.Join(p.dir, name))
	return err == nil
}

// existing makes the directory a clone of the GitHub repository, which
// already holds one commit on main.
func (p *project) existing() {
	p.t.Helper()
	p.fake.SetExists(true)
	p.git("init", "-q")
	p.git("remote", "add", "origin", "https://github.com/"+p.fullName+".git")
	p.git("add", "AGENTS.md")
	p.git("commit", "-q", "-m", "first")
	p.git("push", "-q", "origin", "main")
}

var allTools = func(name string, args ...string) (string, error) {
	out := map[string]string{
		"git --version":          "git version 2.47.1\n",
		"podman --version":       "podman version 5.7.0\n",
		"podman compose version": "podman-compose version 1.2.0\n",
	}[strings.Join(append([]string{name}, args...), " ")]
	if out == "" {
		return "", fmt.Errorf("exec: %q: %w", name, exec.ErrNotFound)
	}
	return out, nil
}

func (p *project) run(args ...string) (code int, stdout, stderr string) {
	p.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Version: "v1.2.3", Stdout: &out, Stderr: &errOut, Clock: p.clock,
		Dir: p.dir, Getenv: func(k string) string { return p.env[k] }, GH: p.fake.Run, Probe: allTools,
		Compose: p.compose, EventsPoll: poll, EventsTest: 5 * time.Second,
	}
	code = Run(args, env)
	return code, out.String(), errOut.String()
}

// mustInit runs crew init and fails the test unless it succeeds.
func (p *project) mustInit(args ...string) string {
	p.t.Helper()
	code, out, errOut := p.run(append([]string{"init"}, args...)...)
	if code != 0 || errOut != "" {
		p.t.Fatalf("crew init: code=%d stderr=%q stdout=\n%s", code, errOut, out)
	}
	outputtest.CheckTerse(p.t, out)
	return out
}

// committed lists the files in the commit at origin's main.
func (p *project) committed() []string {
	p.t.Helper()
	return strings.Fields(gitIn(p.t, p.origin, "show", "--name-only", "--format=", "main"))
}

func (p *project) labelNames() []string {
	p.t.Helper()
	var names []string
	res, _ := p.fake.Run([]string{"api", "--method", "GET", "repos/" + p.fullName + "/labels?per_page=100"}, nil)
	var ls []struct{ Name string }
	json.Unmarshal(res.Stdout, &ls)
	for _, l := range ls {
		names = append(names, l.Name)
	}
	return names
}

func (p *project) writes() []string {
	var w []string
	for _, r := range p.fake.Requests() {
		if !strings.HasPrefix(r, "GET ") {
			w = append(w, r)
		}
	}
	return w
}

var crewFiles = []string{
	".crew/crew.toml",
	".crew/docs/adr/0001-python-stack.md",
	".crew/docs/architecture.md",
	".crew/docs/conventions.md",
	".crew/docs/product.md",
	".gitignore",
	"Containerfile",
	"compose.yaml",
	"pyproject.toml",
	"src/app/__init__.py",
	"tests/test_app.py",
}

// #4-AC1 #4-AC4 #4-AC5 #4-AC6 #4-AC7
func TestInitNewProject(t *testing.T) {
	p := newProject(t, "user/app")
	p.env["AI_AGENT"] = "claude-code_2-1-296_agent"
	out := p.mustInit("--profile", "python")

	want := "repo user/app created private\nlabels created 26\n"
	for _, f := range crewFiles {
		want += "wrote " + f + "\n"
	}
	sha := gitIn(t, p.origin, "rev-parse", "main")
	want += "commit " + sha[:7] + " pushed main\n"
	want += "ok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nok gh-auth user\nok config python\nok tiers 1-3\nok labels 26\nok knowledge 801 bytes\nok rules\n"
	want += "warn harness: claude-code has no wiring in .crew/crew.toml; the lead DM runs the self-test at its next start\nok gate " + sha[:7] + "\n"
	if out != want {
		t.Fatalf("got\n%swant\n%s", out, want)
	}
	if !p.fake.Exists() || !p.fake.Private() {
		t.Errorf("repo exists=%v private=%v", p.fake.Exists(), p.fake.Private())
	}
	if got := p.git("config", "--get", "remote.origin.url"); got != "https://github.com/user/app.git" {
		t.Errorf("origin %q", got)
	}
	if got := p.git("rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("branch %q", got)
	}
	if got := p.committed(); !slices.Equal(got, crewFiles) {
		t.Errorf("committed %v\nwant %v", got, crewFiles)
	}
	if msg := gitIn(t, p.origin, "log", "-1", "--format=%s", "main"); msg != "Set up Skeleton Crew" {
		t.Errorf("message %q", msg)
	}
	if got := p.read(".crew/crew.toml"); got != string(config.Template("python", "claude-code", "release")) {
		t.Errorf("crew.toml:\n%s", got)
	}
	ignore := p.read(".gitignore")
	for _, line := range []string{".crew/worktrees/", ".crew/logs/", ".crew/lead.lock*", ".crew/events.*", "__pycache__/"} {
		if !slices.Contains(strings.Split(ignore, "\n"), line) {
			t.Errorf(".gitignore lacks %s:\n%s", line, ignore)
		}
	}
	// AGENTS.md is the user's: it stays untracked.
	if st := p.git("status", "--porcelain"); st != "?? AGENTS.md" {
		t.Errorf("status %q", st)
	}
}

// #4-AC1
func TestInitNewRepositoryOptions(t *testing.T) {
	p := newProject(t, "team/web")
	p.fake.SetGitProtocol("ssh")
	out := p.mustInit("--profile", "python", "--repo", "team/web", "--public")
	if !strings.HasPrefix(out, "repo team/web created public\n") {
		t.Errorf("output:\n%s", out)
	}
	if p.fake.Private() {
		t.Error("repo is private")
	}
	if !slices.Contains(p.fake.Requests(), "POST /orgs/team/repos") {
		t.Errorf("requests %v", p.fake.Requests())
	}
	if got := p.git("config", "--get", "remote.origin.url"); got != "git@github.com:team/web.git" {
		t.Errorf("origin %q", got)
	}
}

// #4-AC1
func TestInitRepositoryNameDefaultsToDirectory(t *testing.T) {
	p := newProject(t, "user/app")
	p.mustInit("--profile", "python")
	if !slices.Contains(p.fake.Requests(), "POST /user/repos") {
		t.Errorf("requests %v", p.fake.Requests())
	}
}

// #4-AC1
func TestInitGitRepositoryWithoutOrigin(t *testing.T) {
	p := newProject(t, "user/app")
	p.git("init", "-q")
	p.mustInit("--profile", "python")
	if !p.fake.Exists() {
		t.Error("repo not created")
	}
	if got := p.git("config", "--get", "remote.origin.url"); got != "https://github.com/user/app.git" {
		t.Errorf("origin %q", got)
	}
}

// #4-AC2 #4-AC6 #4-AC7
func TestInitExistingProject(t *testing.T) {
	p := newProject(t, "user/app")
	p.existing()
	p.write("main.py", "print('hi')\n")
	p.git("add", "main.py")
	p.git("commit", "-q", "-m", "app")
	p.git("push", "-q", "origin", "main")
	p.write("Containerfile", "FROM mine\n")
	p.git("add", "Containerfile")
	p.git("commit", "-q", "-m", "container")
	p.git("push", "-q", "origin", "main")
	// The user's work in progress.
	p.write("main.py", "print('changed')\n")
	p.write("notes.txt", "draft\n")
	p.write("staged.txt", "staged\n")
	p.git("add", "staged.txt")

	out := p.mustInit("--profile", "python")
	if !strings.HasPrefix(out, "repo user/app\n") {
		t.Errorf("output:\n%s", out)
	}
	if slices.ContainsFunc(p.fake.Requests(), func(r string) bool { return strings.Contains(r, "/repos ") || strings.HasSuffix(r, "/repos") }) {
		t.Errorf("created a repo: %v", p.fake.Requests())
	}
	want := []string{".crew/crew.toml", ".crew/docs/adr/0001-python-stack.md", ".crew/docs/architecture.md", ".crew/docs/conventions.md", ".crew/docs/product.md", ".gitignore"}
	if got := p.committed(); !slices.Equal(got, want) {
		t.Errorf("committed %v\nwant %v", got, want)
	}
	if p.read("Containerfile") != "FROM mine\n" {
		t.Error("Containerfile was changed")
	}
	for _, f := range []string{"compose.yaml", "pyproject.toml", "src", "tests"} {
		if p.exists(f) {
			t.Errorf("wrote %s into an existing project", f)
		}
	}
	if ignore := p.read(".gitignore"); strings.Contains(ignore, "__pycache__") {
		t.Errorf("scaffold ignores in an existing project:\n%s", ignore)
	}
	if st := p.git("status", "--porcelain"); st != "M main.py\nA  staged.txt\n?? notes.txt" {
		t.Errorf("status %q", st)
	}
	if p.read("main.py") != "print('changed')\n" {
		t.Error("user's change lost")
	}
}

// #4-AC5
func TestInitAppendsToExistingGitignore(t *testing.T) {
	p := newProject(t, "user/app")
	p.existing()
	p.write("main.py", "print('hi')\n")
	p.write(".gitignore", "dist/\n.crew/logs/\n")
	p.git("add", "main.py", ".gitignore")
	p.git("commit", "-q", "-m", "ignore")
	p.git("push", "-q", "origin", "main")
	p.mustInit("--profile", "python")
	if got := p.read(".gitignore"); got != "dist/\n.crew/logs/\n.crew/worktrees/\n.crew/lead.lock*\n.crew/events.*\n" {
		t.Errorf(".gitignore:\n%s", got)
	}
}

// #4-AC3
func TestInitCreatesLabels(t *testing.T) {
	p := newProject(t, "user/app")
	p.mustInit("--profile", "python")
	var want []string
	for _, l := range labels.All([]int{1, 2, 3}) {
		want = append(want, l.Name)
	}
	if got := p.labelNames(); !slices.Equal(got, want) {
		t.Errorf("labels %v\nwant %v", got, want)
	}
}

// #4-AC3: an existing label is kept as it is.
func TestInitKeepsExistingLabels(t *testing.T) {
	p := newProject(t, "user/app")
	p.existing()
	p.fake.Run([]string{"api", "--method", "POST", "repos/user/app/labels", "--input", "-"}, []byte(`{"name":"blocked","color":"000000"}`))
	out := p.mustInit("--profile", "python")
	if !strings.Contains(out, "labels created 25\n") {
		t.Errorf("output:\n%s", out)
	}
}

// #4-AC3: running it again changes nothing.
func TestInitAgainChangesNothing(t *testing.T) {
	p := newProject(t, "user/app")
	p.mustInit("--profile", "python")
	before := gitIn(t, p.origin, "rev-parse", "main")
	writes := len(p.writes())

	out := p.mustInit("--profile", "python")
	want := "repo user/app\nlabels created 0\ncommit none\nok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nok gh-auth user\nok config python\nok tiers 1-3\nok labels 26\nok knowledge 801 bytes\nok rules\nwarn harness: unknown; crew cannot tell which harness runs it\nok gate " + before[:7] + "\n"
	if out != want {
		t.Errorf("got\n%swant\n%s", out, want)
	}
	if got := gitIn(t, p.origin, "rev-parse", "main"); got != before {
		t.Error("made a new commit")
	}
	if w := p.writes(); len(w) != writes {
		t.Errorf("wrote to GitHub again: %v", w[writes:])
	}
}

// #4-AC3: tier labels follow the configured ranks.
func TestInitTierLabelsFollowConfig(t *testing.T) {
	p := newProject(t, "user/app")
	p.existing()
	p.write(".crew/crew.toml", "profile = \"python\"\n[tiers.1]\neffort = \"low\"\n[tiers.2]\neffort = \"low\"\n[tiers.3]\neffort = \"low\"\n[tiers.4]\neffort = \"max\"\n")
	p.git("add", ".crew/crew.toml")
	p.git("commit", "-q", "-m", "config")
	p.git("push", "-q", "origin", "main")
	out := p.mustInit("--profile", "python")
	if !slices.Contains(p.labelNames(), "tier:4") {
		t.Errorf("no tier:4: %v", p.labelNames())
	}
	if strings.Contains(out, "wrote .crew/crew.toml") || !strings.Contains(p.read(".crew/crew.toml"), "tiers.4") {
		t.Error("crew.toml was overwritten")
	}
}

// #4-AC4
func TestInitHarness(t *testing.T) {
	for _, tc := range []struct {
		env     map[string]string
		args    []string
		harness string
	}{
		{map[string]string{"CLAUDECODE": "1"}, nil, "claude-code"},
		{map[string]string{"OPENCODE": "1"}, nil, "opencode"},
		{map[string]string{}, nil, ""},
		{map[string]string{}, []string{"--harness", "claude-code"}, "claude-code"},
		{map[string]string{"CLAUDECODE": "1"}, []string{"--harness", "codex"}, "codex"},
	} {
		p := newProject(t, "user/app")
		p.env = tc.env
		p.mustInit(append([]string{"--profile", "python"}, tc.args...)...)
		if got := p.read(".crew/crew.toml"); got != string(config.Template("python", tc.harness, "release")) {
			t.Errorf("%v %v: crew.toml:\n%s", tc.env, tc.args, got)
		}
	}
}

// #4-AC2: outside the normal path it changes nothing and says why.
func TestInitStops(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(p *project)
		want  string
	}{
		{"origin elsewhere", func(p *project) {
			p.git("init", "-q")
			p.git("remote", "add", "origin", "https://gitlab.com/user/app.git")
		}, "origin is not on github.com: https://gitlab.com/user/app.git\n"},
		{"not on main", func(p *project) {
			p.existing()
			p.git("switch", "-q", "-c", "work")
		}, "checked out branch is work, not main\n"},
		{"gitignore changed", func(p *project) {
			p.existing()
			p.write(".gitignore", "mine\n")
		}, ".gitignore has uncommitted changes\n"},
		{"diverged", func(p *project) {
			p.existing()
			p.git("commit", "-q", "--allow-empty", "-m", "local only")
		}, "local main has 1 commit that origin/main does not have\n"},
		{"subdirectory", func(p *project) {
			p.git("init", "-q")
			p.dir = filepath.Join(p.dir, "sub")
			os.Mkdir(p.dir, 0o755)
		}, "not at the root of the git repository: "},
		{"bad crew.toml", func(p *project) {
			p.existing()
			p.write(".crew/crew.toml", "profile = 1.5\n")
		}, ".crew/crew.toml:1: unsupported value\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProject(t, "user/app")
			tc.setup(p)
			before := p.writes()
			code, out, errOut := p.run("init", "--profile", "python")
			if code != 1 || out != "" || !strings.HasPrefix(errOut, tc.want) || strings.Count(errOut, "\n") != 1 {
				t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
			}
			if w := p.writes(); len(w) != len(before) {
				t.Errorf("wrote to GitHub: %v", w[len(before):])
			}
			if p.exists(".crew/docs") {
				t.Error("wrote crew files")
			}
		})
	}
}

// #4-AC1: a failure on GitHub stops crew init before it changes anything
// locally.
func TestInitStopsOnGitHubError(t *testing.T) {
	p := newProject(t, "user/app")
	p.fake.SetLoggedIn(false)
	code, out, errOut := p.run("init", "--profile", "python")
	if code != 1 || out != "" || errOut != "gh failed: To get started with GitHub CLI, please run: gh auth login\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if p.exists(".git") || p.exists(".crew") {
		t.Error("changed the directory")
	}
}

// #4-AC8
func TestInitTakesFlagsOnly(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "missing flag: --profile\n"},
		{[]string{"--profile", "cobol"}, "unknown profile: cobol\n"},
		{[]string{"--profile", "python", "extra"}, "unexpected argument: extra\n"},
		{[]string{"--profile", "python", "--bogus"}, "unknown flag: --bogus\n"},
		{[]string{"--profile", "python", "--repo", "app"}, "invalid --repo: app; want <owner>/<name>\n"},
	} {
		p := newProject(t, "user/app")
		code, out, errOut := p.run(append([]string{"init"}, tc.args...)...)
		if code != 2 || out != "" || errOut != tc.want {
			t.Errorf("%v: got code=%d stdout=%q stderr=%q", tc.args, code, out, errOut)
		}
		if len(p.fake.Requests()) != 0 || p.exists(".git") {
			t.Errorf("%v: acted on bad flags", tc.args)
		}
	}
}

// #4-AC9: crew init ends with the doctor's checks, and fails if one fails.
func TestInitReportsFailedCheck(t *testing.T) {
	p := newProject(t, "user/app")
	var out, errOut bytes.Buffer
	env := Env{
		Stdout: &out, Stderr: &errOut, Clock: clock.NewFake(clock.Epoch), Dir: p.dir,
		Getenv: func(string) string { return "" }, GH: p.fake.Run,
		Probe: func(name string, args ...string) (string, error) {
			if name == "git" {
				return allTools(name, args...)
			}
			return "", fmt.Errorf("exec: %q: %w", name, exec.ErrNotFound)
		},
	}
	if code := Run([]string{"init", "--profile", "python"}, env); code != 1 {
		t.Fatalf("code %d", code)
	}
	if !strings.HasSuffix(out.String(), "fail container-engine: install podman or docker, with compose\nok gh 2.46.0\nok gh-auth user\nok config python\nok tiers 1-3\nok labels 26\nok knowledge 801 bytes\nok rules\nwarn harness: unknown; crew cannot tell which harness runs it\nfail gate: not run; needs a container engine\n") {
		t.Errorf("stdout:\n%s", out.String())
	}
	if errOut.String() != "" {
		t.Errorf("stderr %q", errOut.String())
	}
}

// #4-AC1
func TestInitJSON(t *testing.T) {
	p := newProject(t, "user/app")
	out := p.mustInit("--profile", "python", "--json")
	var got struct {
		Repo       string   `json:"repo"`
		Created    bool     `json:"created"`
		Visibility string   `json:"visibility"`
		Labels     []string `json:"labels"`
		Wrote      []string `json:"wrote"`
		Commit     string   `json:"commit"`
		Checks     []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if strings.Count(out, "\n") != 1 || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("not one line of JSON:\n%s", out)
	}
	if got.Repo != "user/app" || !got.Created || got.Visibility != "private" || len(got.Labels) != 26 || !slices.Equal(got.Wrote, crewFiles) || got.Commit != gitIn(t, p.origin, "rev-parse", "main") || len(got.Checks) != 11 {
		t.Errorf("got %+v", got)
	}
}

// #4-AC3: GitHub label names are case-insensitive.
func TestInitKeepsLabelsThatDifferInCase(t *testing.T) {
	p := newProject(t, "user/app")
	p.existing()
	p.fake.Run([]string{"api", "--method", "POST", "repos/user/app/labels", "--input", "-"}, []byte(`{"name":"Blocked"}`))
	out := p.mustInit("--profile", "python")
	if !strings.Contains(out, "labels created 25\n") {
		t.Errorf("output:\n%s", out)
	}
}

// #4-AC1: the login is case-insensitive too.
func TestInitRepoUnderOwnLoginInOtherCase(t *testing.T) {
	p := newProject(t, "user/app")
	p.mustInit("--profile", "python", "--repo", "User/app")
	if !slices.Contains(p.fake.Requests(), "POST /user/repos") {
		t.Errorf("requests %v", p.fake.Requests())
	}
}

// #4-AC1: a run whose push failed is finished by the next run.
func TestInitAgainPushesWhatAFailedPushLeft(t *testing.T) {
	p := newProject(t, "user/app")
	// The remote disappears after the repository is created, so only the
	// push fails.
	aside := p.origin + ".aside"
	p.fake.Hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/labels") {
			os.Rename(p.origin, aside)
		}
		return false
	}
	if code, _, errOut := p.run("init", "--profile", "python"); code != 1 || !strings.HasPrefix(errOut, "git failed: git push") {
		t.Fatalf("push did not fail: code %d stderr %q", code, errOut)
	}
	p.fake.Hook = nil
	os.Rename(aside, p.origin)
	out := p.mustInit("--profile", "python")
	local := p.git("rev-parse", "main")
	if got := gitIn(t, p.origin, "rev-parse", "main"); got != local {
		t.Fatalf("origin main %s, local %s", got, local)
	}
	if !strings.Contains(out, "commit "+local[:7]+" pushed main\n") {
		t.Errorf("output:\n%s", out)
	}
}

// #4-AC2: the profile of an existing crew.toml wins over a different flag.
func TestInitStopsOnOtherProfile(t *testing.T) {
	p := newProject(t, "user/app")
	p.existing()
	p.write(".crew/crew.toml", "profile = \"node-ts\"\n[tiers.1]\neffort = \"low\"\n")
	p.git("add", ".crew/crew.toml")
	p.git("commit", "-q", "-m", "config")
	p.git("push", "-q", "origin", "main")
	code, out, errOut := p.run("init", "--profile", "python")
	if code != 1 || out != "" || errOut != ".crew/crew.toml has profile node-ts, not python\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// #15-AC4: crew init takes a custom profile written into the project like a
// built-in one, and commits it with the files it writes.
func TestInitCustomProfile(t *testing.T) {
	p := newProject(t, "user/app")
	p.write(".crew/profiles/elixir/profile.toml", "[commands]\ntest = \"mix test\"\ntest-changed = \"mix test\"\n[tests]\nglobs = [\"test/**/*_test.exs\"]\n")
	p.write(".crew/profiles/elixir/docs/conventions.md", "# Conventions\n")
	p.write(".crew/profiles/elixir/scaffold/compose.yaml", "services:\n  app:\n")
	p.write(".crew/profiles/elixir/scaffold/.gitignore", "_build/\n")
	out := p.mustInit("--profile", "elixir")
	for _, want := range []string{"wrote .crew/docs/conventions.md\n", "wrote compose.yaml\n", "added .crew/profiles/elixir\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	want := []string{".crew/crew.toml", ".crew/docs/conventions.md", ".crew/profiles/elixir/docs/conventions.md", ".crew/profiles/elixir/profile.toml", ".crew/profiles/elixir/scaffold/.gitignore", ".crew/profiles/elixir/scaffold/compose.yaml", ".gitignore", "compose.yaml"}
	if got := p.committed(); !slices.Equal(got, want) {
		t.Errorf("committed %v\nwant %v", got, want)
	}
	if !strings.Contains(p.read(".crew/crew.toml"), "profile = \"elixir\"\n") || !strings.Contains(p.read(".gitignore"), "_build/\n") {
		t.Errorf("crew.toml or .gitignore")
	}
	// Run again, it changes nothing.
	if out := p.mustInit("--profile", "elixir"); !strings.Contains(out, "commit none\n") || strings.Contains(out, "added") {
		t.Errorf("second run:\n%s", out)
	}
}

func TestInitCustomProfileRefusals(t *testing.T) {
	p := newProject(t, "user/app")
	p.write(".crew/profiles/broken/profile.toml", "[commands]\ntest = \"t\"\n")
	if code, out, errOut := p.run("init", "--profile", "broken"); code != 1 || out != "" || errOut != ".crew/profiles/broken/profile.toml: commands.test-changed is missing\n" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if code, _, errOut := p.run("init", "--profile", "cobol"); code != 2 || errOut != "unknown profile: cobol\n" {
		t.Fatalf("got code=%d stderr=%q", code, errOut)
	}
	if w := p.writes(); len(w) != 0 {
		t.Fatalf("writes %v", w)
	}
}
