package doctor

import (
	"fmt"
	"net/http"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/github/fakegithub"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

// tools answers probes like the named tools would; a tool that is not in
// the map is not installed. Each value is the output of "<tool> <args>".
func tools(m map[string]string) Probe {
	return func(name string, args ...string) (string, error) {
		out, ok := m[strings.Join(append([]string{name}, args...), " ")]
		if !ok {
			if _, installed := m[name+" --version"]; installed {
				return "", fmt.Errorf("exit status 125")
			}
			return "", fmt.Errorf("exec: %q: %w", name, exec.ErrNotFound)
		}
		return out, nil
	}
}

var all = map[string]string{
	"git --version":          "git version 2.47.1\n",
	"podman --version":       "podman version 5.7.0\n",
	"podman compose version": "podman-compose version 1.2.0\n",
	"docker --version":       "Docker version 27.3.1, build ce12230\n",
	"docker compose version": "Docker Compose version v2.29.7\n",
}

func without(keys ...string) map[string]string {
	m := map[string]string{}
	for k, v := range all {
		m[k] = v
	}
	for _, k := range keys {
		delete(m, k)
	}
	return m
}

func fake() *fakegithub.GitHub { return fakegithub.New("user/app", clock.NewFake(clock.Epoch)) }

func lines(checks []Check) string {
	var b strings.Builder
	for _, c := range checks {
		b.WriteString(c.String() + "\n")
	}
	return b.String()
}

// #4-AC9
func TestAllPresent(t *testing.T) {
	got := lines(Run(tools(all), fake().Run))
	want := "ok git 2.47.1\nok podman 5.7.0 compose\nok gh 2.46.0\nok gh-auth user\n"
	if got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
	outputtest.CheckTerse(t, got)
}

// #4-AC9
func TestContainerEngine(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools map[string]string
		want  string
	}{
		{"docker only", without("podman --version", "podman compose version"), "ok docker 27.3.1 compose"},
		{"podman without compose", without("podman compose version"), "ok docker 27.3.1 compose"},
		{"no compose at all", without("podman compose version", "docker compose version"), "fail podman: compose is not available"},
		{"none", without("podman --version", "podman compose version", "docker --version", "docker compose version"), "fail container-engine: install podman or docker, with compose"},
	} {
		got := Run(tools(tc.tools), fake().Run)
		if s := got[1].String(); s != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, s, tc.want)
		}
	}
}

// #4-AC9
func TestMissingTools(t *testing.T) {
	missingGh := func([]string, []byte) (github.Result, error) {
		return github.Result{}, fmt.Errorf("exec: \"gh\": %w", exec.ErrNotFound)
	}
	got := lines(Run(tools(without("git --version")), missingGh))
	want := "fail git: not installed\nok podman 5.7.0 compose\nfail gh: not installed\n"
	if got != want {
		t.Fatalf("got\n%swant\n%s", got, want)
	}
}

// #4-AC9
func TestAuthMissingPrintsCommandForTheUser(t *testing.T) {
	f := fake()
	f.SetLoggedIn(false)
	checks := Run(tools(all), f.Run)
	last := checks[len(checks)-1]
	if s := last.String(); s != "fail gh-auth: run in your own terminal: gh auth login" {
		t.Fatalf("got %q", s)
	}
}

// #4-AC9
func TestAuthOtherFailureIsReported(t *testing.T) {
	f := fake()
	f.Hook = func(w http.ResponseWriter, r *http.Request) bool {
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	}
	checks := Run(tools(all), f.Run)
	want := "fail gh-auth: unexpected github response: GET user: HTTP 503"
	if s := checks[len(checks)-1].String(); s != want {
		t.Fatalf("got %q, want %q", s, want)
	}
}

// #4-AC9
func TestFailed(t *testing.T) {
	if Failed([]Check{Passed("git", ""), Warning("knowledge", "big")}) {
		t.Error("ok and warn count as failed")
	}
	if !Failed([]Check{Passed("git", ""), Failure("gh", "not installed")}) {
		t.Error("a failure does not count")
	}
}

// #4-AC9
func TestCheckJSONFields(t *testing.T) {
	typ := reflect.TypeOf(Check{})
	var tags []string
	for i := range typ.NumField() {
		tags = append(tags, typ.Field(i).Tag.Get("json"))
	}
	if strings.Join(tags, ",") != "name,status,detail" {
		t.Fatalf("tags %v", tags)
	}
}
