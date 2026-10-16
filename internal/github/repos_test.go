package github_test

import (
	"errors"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/github/fakegithub"
)

// #4-AC1 #4-AC9
func TestUser(t *testing.T) {
	fake := fakegithub.New("user/project", clock.NewFake(clock.Epoch))
	u, err := github.New("", fake.Run).User()
	if err != nil || u.Login != fakegithub.Login {
		t.Fatalf("got %+v %v", u, err)
	}
}

// #4-AC1
func TestCreateRepo(t *testing.T) {
	for _, tc := range []struct {
		repo, org, path string
	}{
		{"user/app", "", "POST /user/repos"},
		{"team/app", "team", "POST /orgs/team/repos"},
	} {
		fake := fakegithub.New(tc.repo, clock.NewFake(clock.Epoch))
		fake.SetExists(false)
		c := github.New("", fake.Run)
		r, err := c.CreateRepo(tc.org, "app", true)
		if err != nil {
			t.Fatalf("%s: %v", tc.repo, err)
		}
		want := github.Repo{FullName: tc.repo, Private: true, CloneURL: "https://github.com/" + tc.repo + ".git", SSHURL: "git@github.com:" + tc.repo + ".git"}
		if r != want {
			t.Errorf("%s: got %+v, want %+v", tc.repo, r, want)
		}
		if got := fake.Requests(); len(got) != 1 || got[0] != tc.path {
			t.Errorf("%s: requests %v", tc.repo, got)
		}
		if !fake.Exists() || !fake.Private() {
			t.Errorf("%s: exists=%v private=%v", tc.repo, fake.Exists(), fake.Private())
		}
	}
}

// #4-AC1
func TestCreateRepoPublic(t *testing.T) {
	fake := fakegithub.New("user/app", clock.NewFake(clock.Epoch))
	fake.SetExists(false)
	r, err := github.New("", fake.Run).CreateRepo("", "app", false)
	if err != nil || r.Private || fake.Private() {
		t.Fatalf("got %+v %v, fake private=%v", r, err, fake.Private())
	}
}

// #4-AC1
func TestCreateRepoThatExists(t *testing.T) {
	fake := fakegithub.New("user/app", clock.NewFake(clock.Epoch))
	_, err := github.New("", fake.Run).CreateRepo("", "app", true)
	want := "unexpected github response: POST user/repos: HTTP 422 Repository creation failed. (name already exists on this account)"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v\nwant %s", err, want)
	}
}

// #4-AC1
func TestMissingRepoIsNotFound(t *testing.T) {
	fake := fakegithub.New("user/app", clock.NewFake(clock.Epoch))
	fake.SetExists(false)
	_, err := github.New("user/app", fake.Run).Labels()
	if err == nil || err.Error() != "unexpected github response: GET repos/user/app/labels: HTTP 404 Not Found" {
		t.Fatalf("got %v", err)
	}
}

// #4-AC9
func TestAuthRequired(t *testing.T) {
	fake := fakegithub.New("user/app", clock.NewFake(clock.Epoch))
	fake.SetLoggedIn(false)
	_, err := github.New("", fake.Run).User()
	if want := "gh failed: To get started with GitHub CLI, please run: gh auth login"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
	if !github.AuthRequired(err) {
		t.Error("AuthRequired is false for gh's exit 4")
	}
	unauthorized := &github.Error{Method: "GET", Path: "user", Status: 401, Detail: "Bad credentials"}
	if !github.AuthRequired(unauthorized) {
		t.Error("AuthRequired is false for HTTP 401")
	}
	for _, other := range []error{errors.New("x"), &github.Error{Status: 404}, &github.Error{Detail: "exec: not found"}} {
		if github.AuthRequired(other) {
			t.Errorf("AuthRequired is true for %v", other)
		}
	}
}

// #4-AC9
func TestVersion(t *testing.T) {
	fake := fakegithub.New("user/app", clock.NewFake(clock.Epoch))
	v, err := github.Version(fake.Run)
	if err != nil || v != fakegithub.Version {
		t.Fatalf("got %q %v", v, err)
	}
	failing := func([]string, []byte) (github.Result, error) {
		return github.Result{}, errors.New(`exec: "gh": executable file not found in $PATH`)
	}
	if _, err := github.Version(failing); err == nil || err.Error() != `gh failed: exec: "gh": executable file not found in $PATH` {
		t.Fatalf("got %v", err)
	}
}

// #4-AC1
func TestGitProtocol(t *testing.T) {
	fake := fakegithub.New("user/app", clock.NewFake(clock.Epoch))
	for _, set := range []string{"", "https", "ssh"} {
		fake.SetGitProtocol(set)
		want := set
		if want == "" {
			want = "https"
		}
		if got, err := github.GitProtocol(fake.Run); err != nil || got != want {
			t.Errorf("%q: got %q %v", set, got, err)
		}
	}
}
