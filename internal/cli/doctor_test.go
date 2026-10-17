package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

// doctorProject returns a project set up by crew init in Claude Code, with
// its wiring recorded and the given containers.
func doctorProject(t *testing.T, box *containers) *project {
	t.Helper()
	p := newProject(t, "user/app")
	p.env["AI_AGENT"] = "claude-code_2-1-296_agent"
	p.mustInit("--profile", "python")
	p.write(".crew/crew.toml", p.read(".crew/crew.toml")+"\n[harness.claude-code]\nwiring = \"wait\"\n")
	p.compose = box.run
	return p
}

// push commits the file name with body and pushes it to origin's main.
func (p *project) push(name, body string) string {
	p.t.Helper()
	p.write(name, body)
	p.git("add", name)
	p.git("commit", "-q", "-m", "Change "+name)
	p.git("push", "-q", "origin", "main")
	return p.git("rev-parse", "HEAD")
}

// #4-AC9 #16-AC1 #16-AC2 #16-AC3 #16-AC4 #16-AC5 #16-AC6
func TestDoctor(t *testing.T) {
	box := &containers{}
	p := doctorProject(t, box)
	sha := p.git("rev-parse", "HEAD")
	code, out, errOut := p.run("doctor")
	want := "ok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nok gh-auth user\n" +
		"ok config python\nok tiers 1-3\nok labels 26\nok knowledge 801 bytes\nok rules\nok harness claude-code wait\nok gate " + sha[:7] + "\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	outputtest.CheckTerse(t, out)
}

// #16-AC6: the gate runs the profile's container steps once on
// origin/main, in its own worktree and compose project, and leaves
// neither behind.
func TestDoctorRunsOneGate(t *testing.T) {
	box := &containers{}
	p := doctorProject(t, box)
	p.ok("doctor")
	dir := filepath.Join(p.dir, ".crew", "worktrees", "gate", "doctor")
	prefix := dir + " -p " + compose.DoctorProject(p.dir) + " "
	var steps []string
	for _, c := range box.calls {
		if !strings.HasPrefix(c, prefix) {
			t.Fatalf("call %q", c)
		}
		steps = append(steps, strings.Fields(strings.TrimPrefix(c, prefix))[0])
	}
	if want := []string{"down", "build", "run", "run", "run", "down"}; !slices.Equal(steps, want) {
		t.Errorf("steps %v, want %v", steps, want)
	}
	if p.exists(".crew/worktrees/gate/doctor") {
		t.Error("left the worktree behind")
	}
	if p.git("worktree", "list", "--porcelain") != "worktree "+p.dir+"\nHEAD "+p.git("rev-parse", "HEAD")+"\nbranch refs/heads/main" {
		t.Error("left a worktree registered")
	}
	if slices.ContainsFunc(p.fake.Requests(), func(r string) bool { return strings.Contains(r, "/statuses/") }) {
		t.Error("posted a commit status")
	}
}

// #16-AC6
func TestDoctorGateFails(t *testing.T) {
	box := &containers{answer: func(dir string, args []string) (string, int) {
		if slices.Contains(args, "pytest -q \"$@\"") {
			return "1 failed\n", 1
		}
		return "", 0
	}}
	p := doctorProject(t, box)
	code, out, _ := p.run("doctor")
	logs, _ := os.ReadDir(filepath.Join(p.dir, ".crew", "logs"))
	if len(logs) == 0 || !strings.HasSuffix(logs[len(logs)-1].Name(), "-doctor.log") {
		t.Fatalf("logs %v", logs)
	}
	want := "fail gate: test: exit 1; full output: .crew/logs/" + logs[len(logs)-1].Name() + "\n"
	if code != 1 || !strings.HasSuffix(out, want) {
		t.Fatalf("got code=%d stdout=%q", code, out)
	}
}

// #16-AC1 #16-AC2 #16-AC3 #16-AC4 #16-AC5: every problem is reported, a
// warning alone does not fail, and a failure does.
func TestDoctorReportsProblems(t *testing.T) {
	p := doctorProject(t, &containers{})
	p.env["AI_AGENT"] = "opencode"
	p.push(".crew/docs/conventions.md", strings.Repeat("x", 32000)+"\nRelease with gh release create.\n")
	code, out, _ := p.run("doctor")
	sha := p.git("rev-parse", "HEAD")
	want := "ok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nok gh-auth user\nok config python\nok tiers 1-3\nok labels 26\n" +
		"warn knowledge: 32399 bytes, over the budget of 32000; condense product.md, architecture.md and conventions.md, moving detail into ADRs\n" +
		"warn rules: .crew/docs/conventions.md:2: gh release; agents work through crew, never gh\n" +
		"warn harness: opencode has no wiring in .crew/crew.toml; the lead DM runs the self-test at its next start\n" +
		"ok gate " + sha[:7] + "\n"
	if code != 0 || out != want {
		t.Fatalf("warnings: got code=%d stdout=\n%swant\n%s", code, out, want)
	}
	outputtest.CheckTerse(t, out)

	p.write(".crew/crew.toml", p.read(".crew/crew.toml")+"\n[tiers.5]\neffort = \"max\"\n")
	code, out, _ = p.run("doctor")
	for _, line := range []string{
		"fail tiers: rank 4 is missing; ranks run from 1 upwards with no gaps\n",
		"fail labels: missing tier:5; run crew init\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in\n%s", line, out)
		}
	}
	if code != 1 {
		t.Errorf("code %d", code)
	}
}

// #4-AC9 #16-AC1
func TestDoctorMissingAuth(t *testing.T) {
	p := doctorProject(t, &containers{})
	p.fake.SetLoggedIn(false)
	code, out, errOut := p.run("doctor")
	if code != 1 || errOut != "" || !strings.Contains(out, "\nfail gh-auth: run in your own terminal: gh auth login\nok config python\nok tiers 1-3\nfail labels: not checked; needs gh-auth\n") {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// #16-AC1
func TestDoctorWithoutConfig(t *testing.T) {
	p := newProject(t, "user/app")
	p.git("init", "-q")
	code, out, _ := p.run("doctor")
	if code != 1 || !strings.HasSuffix(out, "\nok gh-auth user\nfail config: no .crew/crew.toml; run crew init\n") {
		t.Fatalf("got code=%d stdout=%q", code, out)
	}
	p.write(".crew/crew.toml", "profile = \"python\"\nbogus = 1\n[tiers.1]\neffort = \"low\"\n")
	code, out, _ = p.run("doctor")
	if code != 1 || !strings.HasSuffix(out, "\nfail config: .crew/crew.toml:2: unknown key bogus\n") {
		t.Fatalf("got code=%d stdout=%q", code, out)
	}
}

// #4-AC9
func TestDoctorJSON(t *testing.T) {
	p := doctorProject(t, &containers{})
	sha := p.git("rev-parse", "HEAD")
	code, out, _ := p.run("doctor", "--json")
	want := `{"checks":[{"name":"git","status":"ok","detail":"2.47.1"},{"name":"podman","status":"ok","detail":"5.7.0 compose"},{"name":"gh","status":"ok","detail":"2.46.0"},{"name":"gh-auth","status":"ok","detail":"user"},` +
		`{"name":"config","status":"ok","detail":"python"},{"name":"tiers","status":"ok","detail":"1-3"},{"name":"labels","status":"ok","detail":"26"},{"name":"knowledge","status":"ok","detail":"801 bytes"},` +
		`{"name":"rules","status":"ok","detail":""},{"name":"harness","status":"ok","detail":"claude-code wait"},{"name":"gate","status":"ok","detail":"` + sha[:7] + `"}]}` + "\n"
	if code != 0 || out != want {
		t.Fatalf("got code=%d stdout=%s", code, out)
	}
}

func TestDoctorRejectsArguments(t *testing.T) {
	p := newProject(t, "user/app")
	if code, _, errOut := p.run("doctor", "x"); code != 2 || errOut != "unexpected argument: x\n" {
		t.Fatalf("got code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut := p.run("doctor", "--bogus"); code != 2 || errOut != "unknown flag: --bogus\n" {
		t.Fatalf("got code=%d stderr=%q", code, errOut)
	}
}
