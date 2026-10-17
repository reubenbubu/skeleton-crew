package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/config"
)

// podmanPS is Podman's compose ps output for an app container that
// publishes container port 3000 at host port host.
func podmanPS(host string) string {
	return `[{"Names":["x_app_1"],"Labels":{"com.docker.compose.service":"app"},"Ports":[{"host_ip":"","container_port":3000,"host_port":` + host + `,"range":1,"protocol":"tcp"}]}]` + "\n"
}

// withMain makes the project use the node-ts profile, which has a run
// command, and answers compose ps with the engine's port until an up with
// the generated ports file has run.
func withMain(p *devProject) {
	p.t.Helper()
	p.write(".crew/crew.toml", string(config.Template("node-ts", "", "release")))
	fixed := false
	p.box.answer = func(dir string, args []string) (string, int) {
		switch {
		case slices.Contains(args, "ps"):
			if fixed {
				return podmanPS("3000"), 0
			}
			return podmanPS("41234"), 0
		case slices.Contains(args, "up"):
			fixed = slices.Contains(args, "-f")
		}
		return "", 0
	}
}

func TestMainUpWritesThePortsOnce(t *testing.T) {
	p := newDevProject(t)
	withMain(p)
	sha := p.git("rev-parse", "HEAD")
	project := compose.MainProject(p.main)
	wt := filepath.Join(p.main, ".crew", "worktrees", "main")
	override := filepath.Join(p.main, ".crew", "worktrees", "main.ports.yaml")
	up := " up -d --build --force-recreate --renew-anon-volumes"
	if out := p.ok("main", "up"); out != "main "+sha[:7]+" up\napp 3000 127.0.0.1:3000\n" {
		t.Fatalf("got %q", out)
	}
	want := []string{
		wt + " -p " + project + up,
		wt + " -p " + project + " ps --format json",
		wt + " -p " + project + " -f compose.yaml -f " + override + up,
		wt + " -p " + project + " ps --format json",
	}
	if !slices.Equal(p.box.calls, want) {
		t.Fatalf("calls %q; want %q", p.box.calls, want)
	}
	if got := p.read(".crew/ports.toml"); got != "# Host ports of the main instance. crew writes this file once and never changes it.\n\n[app]\n3000 = 3000\n" {
		t.Fatalf("ports.toml %q", got)
	}
	if got := p.read(".crew/worktrees/main.ports.yaml"); got != "services:\n  \"app\":\n    ports: !override\n      - \"127.0.0.1:3000:3000\"\n" {
		t.Fatalf("override %q", got)
	}
	if got := gitIn(t, wt, "rev-parse", "HEAD"); got != sha {
		t.Fatalf("worktree at %s; want %s", got, sha)
	}
	// Once written, the file is only read: a changed port is used as it is.
	p.write(".crew/ports.toml", "[app]\n3000 = 3001\n")
	p.box.calls = nil
	p.ok("main", "up")
	if !slices.Equal(p.box.calls, want[2:]) {
		t.Fatalf("calls %q; want %q", p.box.calls, want[2:])
	}
	if got := p.read(".crew/ports.toml"); got != "[app]\n3000 = 3001\n" {
		t.Fatalf("ports.toml changed: %q", got)
	}
	if got := p.read(".crew/worktrees/main.ports.yaml"); !strings.Contains(got, "127.0.0.1:3001:3000") {
		t.Fatalf("override %q", got)
	}
}

func TestMainUpReadsDockerComposePS(t *testing.T) {
	p := newDevProject(t)
	withMain(p)
	p.write(".crew/ports.toml", "")
	p.box.answer = func(dir string, args []string) (string, int) {
		if slices.Contains(args, "ps") {
			return `{"Service":"app","Ports":"0.0.0.0:3000->3000/tcp","Labels":"a=b","Publishers":[{"URL":"0.0.0.0","TargetPort":3000,"PublishedPort":3000},{"URL":"::","TargetPort":3000,"PublishedPort":3000}]}` + "\n" +
				`{"Service":"db","Publishers":[{"URL":"","TargetPort":5432,"PublishedPort":0}]}` + "\n", 0
		}
		return "", 0
	}
	if out := p.ok("main", "up"); !strings.HasSuffix(out, " up\napp 3000 127.0.0.1:3000\n") {
		t.Fatalf("got %q", out)
	}
	// An empty ports file lists no service, so no override is passed.
	if slices.ContainsFunc(p.box.calls, func(c string) bool { return strings.Contains(c, " -f ") }) {
		t.Fatalf("calls %q", p.box.calls)
	}
}

func TestMainUpFailures(t *testing.T) {
	p := newDevProject(t)
	p.refused(1, "no main instance: the python profile has no run command", "main", "up")
	withMain(p)
	p.write(".crew/ports.toml", "[app]\n3000 = \"x\"\n")
	p.failed(1, ".crew/ports.toml:2: not a port", "main", "up")
	p.write(".crew/ports.toml", "[app]\n3000 = 3000\n")
	p.box.answer = func(dir string, args []string) (string, int) { return "Error: port 3000 in use\n", 125 }
	errOut := p.failed(1, "up: exit 125", "main", "up")
	if !strings.HasPrefix(errOut, "Error: port 3000 in use\n") {
		t.Fatalf("stderr %q", errOut)
	}
	p.refused(2, "usage: crew main up|down|logs", "main")
	p.refused(2, "unknown command: main bogus", "main", "bogus")
	p.refused(2, "unknown flag: --volumes", "main", "up", "--volumes")
}

func TestMainDownAndLogs(t *testing.T) {
	p := newDevProject(t)
	withMain(p)
	project := compose.MainProject(p.main)
	if out := p.ok("main", "down"); out != "main down\n" {
		t.Fatalf("got %q", out)
	}
	if !p.exists(".crew/main.off") {
		t.Fatal("main.off not written")
	}
	p.ok("main", "down", "--volumes")
	want := []string{p.main + " -p " + project + " down --remove-orphans", p.main + " -p " + project + " down --remove-orphans --volumes"}
	if !slices.Equal(p.box.calls, want) {
		t.Fatalf("calls %q; want %q", p.box.calls, want)
	}
	p.box.answer = func(dir string, args []string) (string, int) { return "app  | listening\n", 0 }
	if out := p.ok("main", "logs"); out != "app  | listening\n" {
		t.Fatalf("got %q", out)
	}
	withMain(p)
	p.ok("main", "up")
	if p.exists(".crew/main.off") {
		t.Fatal("main.off left after main up")
	}
}

func TestMergeRefreshesTheMainInstance(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	withMain(p)
	p.box.calls = nil
	out := p.merge()
	if want := "merged " + sha[:7] + " into main\n#1 stage=9:done\n" + p.removed() + "main " + sha[:7] + " up\napp 3000 127.0.0.1:3000\n"; out != want {
		t.Fatalf("got %q; want %q", out, want)
	}
	if got := gitIn(t, filepath.Join(p.main, ".crew", "worktrees", "main"), "rev-parse", "HEAD"); got != sha {
		t.Fatalf("main instance at %s; want %s", got, sha)
	}
}

func TestMergeLeavesATurnedOffMainInstance(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	withMain(p)
	p.write(".crew/main.off", "")
	p.box.calls = nil
	if out := p.merge(); out != "merged "+sha[:7]+" into main\n#1 stage=9:done\n"+p.removed() {
		t.Fatalf("got %q", out)
	}
	if len(p.box.calls) != 1 {
		t.Fatalf("compose calls %q", p.box.calls)
	}
}

func TestMergeStandsWhenTheRefreshFails(t *testing.T) {
	p := newDevProject(t)
	sha := p.approved()
	withMain(p)
	p.write(".crew/ports.toml", "[app]\n3000 = 3000\n")
	p.box.answer = func(dir string, args []string) (string, int) {
		if slices.Contains(args, "up") {
			return "Error: port 3000 in use\n", 125
		}
		return "", 0
	}
	code, out, errOut := p.run("merge", "1", "--lead", p.token)
	if code != 0 || out != "merged "+sha[:7]+" into main\n#1 stage=9:done\n"+p.removed() {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
	if errOut != "Error: port 3000 in use\nwarning: main instance not refreshed: up: exit 125; run crew main up\n" {
		t.Fatalf("stderr %q", errOut)
	}
	p.merged(sha)
	if _, err := os.Stat(filepath.Join(p.main, ".crew", "main.off")); !os.IsNotExist(err) {
		t.Fatalf("main.off: %v", err)
	}
}
