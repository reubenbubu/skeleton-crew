package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/upgrade"
)

// upgradeProject is a set-up project whose AGENTS.md is the seed file,
// pinned at v0.1.0, and whose configuration records install.
func upgradeProject(t *testing.T, install string) *project {
	t.Helper()
	p := newTicketProject(t)
	seed, err := os.ReadFile("../../project/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := upgrade.Pinned(seed)
	if err != nil {
		t.Fatal(err)
	}
	seed = []byte(strings.ReplaceAll(string(seed), pinned, "v0.1.0"))
	p.push("AGENTS.md", string(seed))
	p.push(config.Path, string(config.Template("python", "", install)))
	return p
}

// #17-AC4
func TestUpgradeMovesThePin(t *testing.T) {
	p := upgradeProject(t, "source")
	before := p.read("AGENTS.md")
	p.fake.SetLatestRelease(upgrade.Source, "v0.2.0")

	out := p.ok("upgrade")
	commit := p.git("rev-parse", "--short=7", "HEAD")
	want := "pinned v0.2.0 was v0.1.0\ncommit " + commit + " pushed main\ninstall v0.2.0 with install=source as the bootstrap in AGENTS.md says\n"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if got := p.read("AGENTS.md"); got != strings.ReplaceAll(before, "v0.1.0", "v0.2.0") {
		t.Fatalf("AGENTS.md:\n%s", got)
	}
	if got := gitIn(t, p.origin, "log", "-1", "--format=%s", "main"); got != "Upgrade crew to v0.2.0" {
		t.Fatalf("origin main: %q", got)
	}
	if got := gitIn(t, p.origin, "show", "--name-only", "--format=", "main"); got != "AGENTS.md" {
		t.Fatalf("committed %q", got)
	}

	if out := p.ok("upgrade"); out != "up to date v0.2.0\n" {
		t.Fatalf("again: %q", out)
	}
	var res map[string]string
	if err := json.Unmarshal([]byte(p.ok("upgrade", "--json")), &res); err != nil || res["pinned"] != "v0.2.0" || res["install"] != "source" || res["commit"] != "" {
		t.Fatalf("json: %v %v", res, err)
	}
}

// #17-AC4
func TestUpgradeNeverMovesThePinBack(t *testing.T) {
	p := upgradeProject(t, "release")
	p.fake.SetLatestRelease(upgrade.Source, "v0.0.9")
	if out := p.ok("upgrade"); out != "up to date v0.1.0\n" {
		t.Fatalf("got %q", out)
	}
	if got := gitIn(t, p.origin, "log", "-1", "--format=%s", "main"); got != "Change "+config.Path {
		t.Fatalf("committed %q", got)
	}
}

// #17-AC4
func TestUpgradeRefusals(t *testing.T) {
	p := upgradeProject(t, "release")
	p.fake.SetLatestRelease(upgrade.Source, "v0.2.0")
	p.refused(2, "unexpected argument: v0.3.0", "upgrade", "v0.3.0")

	p.write("AGENTS.md", "# Agent instructions\n")
	p.refused(1, "AGENTS.md has uncommitted changes", "upgrade")
	p.push("AGENTS.md", "# Agent instructions\n")
	p.refused(1, upgrade.ErrNoPin.Error(), "upgrade")

	q := upgradeProject(t, "release")
	q.refused(1, "unexpected github response: GET repos/"+upgrade.Source+"/releases/latest: HTTP 404 Not Found", "upgrade")
	q.fake.SetLatestRelease(upgrade.Source, "v0.2.0-rc1")
	q.refused(1, "invalid version: v0.2.0-rc1", "upgrade")

	// A push that origin refuses changes nothing.
	q.fake.SetLatestRelease(upgrade.Source, "v0.2.0")
	before, head := q.read("AGENTS.md"), q.git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(q.origin, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := q.run("upgrade")
	if code != 1 || out != "" || !strings.Contains(errOut, "push") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if q.read("AGENTS.md") != before || q.git("rev-parse", "HEAD") != head || q.git("status", "--porcelain") != "" {
		t.Fatalf("changed: status %q", q.git("status", "--porcelain"))
	}
}

// #17-AC4
func TestUpgradeChangesAnUntrackedAgentsFileInPlace(t *testing.T) {
	p := upgradeProject(t, "release")
	p.git("rm", "-q", "--cached", "AGENTS.md")
	p.git("commit", "-q", "-m", "Untrack AGENTS.md")
	p.git("push", "-q", "origin", "main")
	head := p.git("rev-parse", "HEAD")
	p.fake.SetLatestRelease(upgrade.Source, "v0.2.0")
	if out := p.ok("upgrade"); out != "pinned v0.2.0 was v0.1.0\ninstall v0.2.0 with install=release as the bootstrap in AGENTS.md says\n" {
		t.Fatalf("got %q", out)
	}
	if v, err := upgrade.Pinned([]byte(p.read("AGENTS.md"))); v != "v0.2.0" || err != nil {
		t.Fatalf("pinned %q %v", v, err)
	}
	if p.git("rev-parse", "HEAD") != head || p.git("status", "--porcelain") != "?? AGENTS.md" {
		t.Fatalf("committed: status %q", p.git("status", "--porcelain"))
	}
}

// #17-AC4
func TestInitRecordsTheInstallMethod(t *testing.T) {
	p := newProject(t, "user/app")
	p.mustInit("--profile", "python", "--install", "source")
	if !strings.Contains(p.read(config.Path), "install = \"source\"") {
		t.Fatalf("crew.toml:\n%s", p.read(config.Path))
	}
	q := newProject(t, "user/app")
	q.refused(2, "invalid --install: curl; want release, source or go", "init", "--profile", "python", "--install", "curl")
}
