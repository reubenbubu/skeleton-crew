package cli

import (
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

// #4-AC9
func TestDoctor(t *testing.T) {
	p := newProject(t, "user/app")
	code, out, errOut := p.run("doctor")
	want := "ok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nok gh-auth user\n"
	if code != 0 || out != want || errOut != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	outputtest.CheckTerse(t, out)
}

// #4-AC9
func TestDoctorMissingAuth(t *testing.T) {
	p := newProject(t, "user/app")
	p.fake.SetLoggedIn(false)
	code, out, errOut := p.run("doctor")
	want := "ok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nfail gh-auth: run in your own terminal: gh auth login\n"
	if code != 1 || out != want || errOut != "" {
		t.Fatalf("got code=%d stdout=%q stderr=%q", code, out, errOut)
	}
}

// #4-AC9
func TestDoctorJSON(t *testing.T) {
	p := newProject(t, "user/app")
	code, out, _ := p.run("doctor", "--json")
	want := `{"checks":[{"name":"git","ok":true,"detail":"2.47.1"},{"name":"podman","ok":true,"detail":"5.7.0 compose"},{"name":"gh","ok":true,"detail":"2.46.0"},{"name":"gh-auth","ok":true,"detail":"user"}]}` + "\n"
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
