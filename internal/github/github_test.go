package github_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/github/fakegithub"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
)

func setup(t *testing.T) (*github.Client, *fakegithub.GitHub) {
	t.Helper()
	fake := fakegithub.New("user/project", clock.NewFake(clock.Epoch))
	return github.New("user/project", fake.Run), fake
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func names(ls []github.Label) []string {
	var n []string
	for _, l := range ls {
		n = append(n, l.Name)
	}
	return n
}

// #3-AC2
func TestIssues(t *testing.T) {
	c, _ := setup(t)
	created := must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "Login", Body: "## Goal", Labels: []string{"stage:2:drafting"}}))
	if created.Number != 1 || created.ID == 0 || created.Title != "Login" || created.Body != "## Goal" || created.State != "open" {
		t.Fatalf("created %+v", created)
	}
	if !created.CreatedAt.Equal(clock.Epoch) || !slices.Equal(names(created.Labels), []string{"stage:2:drafting"}) {
		t.Fatalf("created %+v", created)
	}
	title := "Login with email"
	edited := must[github.Issue](t)(c.EditIssue(1, github.IssueEdit{Title: &title, State: "closed", StateReason: "not_planned"}))
	if edited.Title != title || edited.Body != "## Goal" || edited.State != "closed" || edited.StateReason != "not_planned" {
		t.Fatalf("edited %+v", edited)
	}
	if got := must[github.Issue](t)(c.Issue(1)); got.Title != title {
		t.Fatalf("read back %+v", got)
	}
	for range 120 {
		must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "more"}))
	}
	if all := must[[]github.Issue](t)(c.Issues("all")); len(all) != 121 {
		t.Fatalf("listed %d issues across pages, want 121", len(all))
	}
	if open := must[[]github.Issue](t)(c.Issues("open")); len(open) != 120 {
		t.Fatalf("listed %d open issues, want 120", len(open))
	}
}

// #3-AC2
func TestIssuesLeaveOutPullRequests(t *testing.T) {
	c, fake := setup(t)
	fake.SetBranch("main", "aaa")
	fake.SetBranch("feat/1/x", "bbb")
	must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "ticket"}))
	must[github.PullRequest](t)(c.CreatePullRequest(github.NewPullRequest{Title: "pr", Head: "feat/1/x", Base: "main"}))
	if got := must[[]github.Issue](t)(c.Issues("all")); len(got) != 1 || got[0].Title != "ticket" {
		t.Fatalf("got %+v", got)
	}
}

// #3-AC2
func TestLabels(t *testing.T) {
	c, _ := setup(t)
	if err := c.CreateLabel(github.Label{Name: "blocked", Color: "b60205", Description: "Waiting"}); err != nil {
		t.Fatal(err)
	}
	if got := must[[]github.Label](t)(c.Labels()); len(got) != 1 || got[0] != (github.Label{Name: "blocked", Color: "b60205", Description: "Waiting"}) {
		t.Fatalf("labels %+v", got)
	}
	must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "t", Labels: []string{"stage:3:eligible"}}))
	got := must[[]github.Label](t)(c.AddLabels(1, "blocked", "priority:high"))
	if want := []string{"stage:3:eligible", "blocked", "priority:high"}; !slices.Equal(names(got), want) {
		t.Fatalf("after add: %v, want %v", names(got), want)
	}
	must[[]github.Label](t)(c.AddLabels(1, "x:branch"))
	must[[]github.Label](t)(c.RemoveLabel(1, "x:branch"))
	got = must[[]github.Label](t)(c.RemoveLabel(1, "stage:3:eligible"))
	if want := []string{"blocked", "priority:high"}; !slices.Equal(names(got), want) {
		t.Fatalf("after remove: %v, want %v", names(got), want)
	}
}

// #3-AC2
func TestComments(t *testing.T) {
	c, _ := setup(t)
	must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "t"}))
	body := "<!-- crew:decision -->\nReuse the session table."
	cm := must[github.Comment](t)(c.CreateComment(1, body))
	if cm.Body != body || cm.User.Login != fakegithub.Login || !cm.CreatedAt.Equal(clock.Epoch) {
		t.Fatalf("comment %+v", cm)
	}
	if got := must[[]github.Comment](t)(c.Comments(1)); len(got) != 1 || got[0].Body != body {
		t.Fatalf("comments %+v", got)
	}
}

// #3-AC2
func TestSubIssuesAndDependencies(t *testing.T) {
	c, _ := setup(t)
	parent := must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "parent"}))
	child := must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "child"}))
	blocker := must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "blocker"}))
	if err := c.AddSubIssue(parent.Number, child.ID); err != nil {
		t.Fatal(err)
	}
	if got := must[[]github.Issue](t)(c.SubIssues(parent.Number)); len(got) != 1 || got[0].Number != child.Number {
		t.Fatalf("sub-issues %+v", got)
	}
	if err := c.AddBlockedBy(child.Number, blocker.ID); err != nil {
		t.Fatal(err)
	}
	if got := must[[]github.Issue](t)(c.BlockedBy(child.Number)); len(got) != 1 || got[0].Number != blocker.Number {
		t.Fatalf("blocked by %+v", got)
	}
}

// #3-AC2
func TestPullRequestsAndReviews(t *testing.T) {
	c, fake := setup(t)
	fake.SetBranch("main", "aaa")
	fake.SetBranch("feat/1/login", "bbb")
	pr := must[github.PullRequest](t)(c.CreatePullRequest(github.NewPullRequest{Title: "Login", Body: "Closes #1", Head: "feat/1/login", Base: "main", Draft: true}))
	if pr.Number != 1 || !pr.Draft || pr.Head != (github.Ref{Ref: "feat/1/login", SHA: "bbb"}) || pr.Base.Ref != "main" {
		t.Fatalf("pr %+v", pr)
	}
	if got := must[github.PullRequest](t)(c.PullRequest(1)); got.Title != "Login" || got.Merged {
		t.Fatalf("read back %+v", got)
	}
	if got := must[[]github.PullRequest](t)(c.PullRequests("feat/1/login", "open")); len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("by head %+v", got)
	}
	if got := must[[]github.PullRequest](t)(c.PullRequests("feat/2/other", "all")); len(got) != 0 {
		t.Fatalf("other head %+v", got)
	}
	rv := must[github.Review](t)(c.CreateReview(1, github.NewReview{Event: "COMMENT", Body: "looks right", CommitID: "bbb"}))
	if rv.State != "COMMENTED" || rv.CommitID != "bbb" || rv.Body != "looks right" {
		t.Fatalf("review %+v", rv)
	}
	if got := must[[]github.Review](t)(c.Reviews(1)); len(got) != 1 || got[0].ID != rv.ID {
		t.Fatalf("reviews %+v", got)
	}
}

// #3-AC2
func TestCommitStatuses(t *testing.T) {
	c, fake := setup(t)
	fake.SetBranch("feat/1/x", "bbb")
	for _, s := range []string{"pending", "success"} {
		if err := c.CreateStatus("bbb", github.Status{State: s, Context: "crew/gate", Description: "gate " + s}); err != nil {
			t.Fatal(err)
		}
	}
	got := must[github.CombinedStatus](t)(c.CombinedStatus("bbb"))
	if got.State != "success" || len(got.Statuses) != 1 || got.Statuses[0].Description != "gate success" || !got.Statuses[0].CreatedAt.Equal(clock.Epoch) {
		t.Fatalf("combined %+v", got)
	}
}

// #3-AC2
func TestWorkflowRuns(t *testing.T) {
	c, fake := setup(t)
	fake.AddWorkflowRun("deploy", "main", "aaa", "completed", "success")
	fake.AddWorkflowRun("deploy", "main", "bbb", "in_progress", "")
	got := must[[]github.WorkflowRun](t)(c.WorkflowRuns("bbb"))
	if len(got) != 1 || got[0].Name != "deploy" || got[0].Status != "in_progress" || got[0].Conclusion != "" || got[0].HeadSHA != "bbb" {
		t.Fatalf("runs %+v", got)
	}
}

// #3-AC4
func TestUnexpectedResponseStopsTheClient(t *testing.T) {
	c, fake := setup(t)
	if err := c.CreateLabel(github.Label{Name: "blocked"}); err != nil {
		t.Fatal(err)
	}
	err := c.CreateLabel(github.Label{Name: "blocked"})
	want := "unexpected github response: POST repos/user/project/labels: HTTP 422 Validation Failed (name: already_exists)"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	outputtest.CheckTerse(t, err.Error())
	served := len(fake.Requests())
	if _, err2 := c.Issue(1); err2 != err {
		t.Fatalf("next call returned %v, want the first error", err2)
	}
	if n := len(fake.Requests()); n != served {
		t.Fatalf("client sent %d more requests after an unexpected response", n-served)
	}
}

// #3-AC4
func TestUnexpectedResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"not found", 404, `{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`,
			"unexpected github response: GET repos/user/project/issues/7: HTTP 404 Not Found"},
		{"string errors", 422, `{"message":"Unprocessable Entity","errors":["Review Can not approve your own pull request"]}`,
			"unexpected github response: GET repos/user/project/issues/7: HTTP 422 Unprocessable Entity (Review Can not approve your own pull request)"},
		{"not JSON", 502, "<html>\n  Bad\n  gateway\n</html>",
			"unexpected github response: GET repos/user/project/issues/7: HTTP 502 <html> Bad gateway </html>"},
		{"long body", 500, strings.Repeat("x", 300),
			"unexpected github response: GET repos/user/project/issues/7: HTTP 500 " + strings.Repeat("x", 200)},
		{"empty body", 503, "",
			"unexpected github response: GET repos/user/project/issues/7: HTTP 503"},
		{"invalid JSON on success", 200, `{"number":`,
			"unexpected github response: GET repos/user/project/issues/7: HTTP 200 invalid JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, fake := setup(t)
			fake.Hook = func(w http.ResponseWriter, r *http.Request) bool {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
				return true
			}
			_, err := c.Issue(7)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v\nwant %q", err, tc.want)
			}
		})
	}
}

// #3-AC4
func TestGhFailsBeforeHTTP(t *testing.T) {
	calls := 0
	c := github.New("user/project", func(args []string, stdin []byte) (github.Result, error) {
		calls++
		return github.Result{Stderr: []byte("To get started with GitHub CLI, please run:  gh auth login\nAlternatively, populate the GH_TOKEN environment variable.\n"), Code: 4}, nil
	})
	_, err := c.Issue(1)
	if want := "gh failed: To get started with GitHub CLI, please run: gh auth login"; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
	c.Issue(1)
	if calls != 1 {
		t.Fatalf("gh ran %d times, want 1", calls)
	}
}

// #3 risk: check-and-set (#6) is built on read-then-write, so the fake must
// let a test change labels between the read and the write.
func TestHookSimulatesLabelRace(t *testing.T) {
	c, fake := setup(t)
	must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "t", Labels: []string{"stage:5:review-approved"}}))
	read := must[github.Issue](t)(c.Issue(1))
	fake.Hook = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost {
			fake.SetIssueLabels(1, "stage:0:cancelled")
		}
		return false
	}
	must[[]github.Label](t)(c.AddLabels(1, "stage:6:pending-merge"))
	if names(read.Labels)[0] != "stage:5:review-approved" {
		t.Fatalf("read %v", names(read.Labels))
	}
	if got, want := fake.IssueLabels(1), []string{"stage:0:cancelled", "stage:6:pending-merge"}; !slices.Equal(got, want) {
		t.Fatalf("labels %v, want %v", got, want)
	}
}

// #3-AC1
func TestRequestsAreGhAPICalls(t *testing.T) {
	var got [][]string
	var stdin []string
	fake := fakegithub.New("user/project", clock.NewFake(clock.Epoch))
	c := github.New("user/project", func(args []string, in []byte) (github.Result, error) {
		got = append(got, args)
		stdin = append(stdin, string(in))
		return fake.Run(args, in)
	})
	must[github.Issue](t)(c.CreateIssue(github.NewIssue{Title: "t"}))
	must[github.Issue](t)(c.Issue(1))
	want := [][]string{
		{"api", "--include", "--method", "POST", "repos/user/project/issues", "--input", "-"},
		{"api", "--include", "--method", "GET", "repos/user/project/issues/1"},
	}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("args %q\nwant %q", got, want)
	}
	if stdin[0] != `{"title":"t","body":""}` || stdin[1] != "" {
		t.Fatalf("stdin %q", stdin)
	}
}

// #3-AC2: gh expands :owner, :repo and :branch in the endpoint, so a label
// name must not reach it with a bare colon.
func TestLabelNameIsNotExpandedByGh(t *testing.T) {
	var path string
	c := github.New("user/project", func(args []string, in []byte) (github.Result, error) {
		path = args[4]
		return github.Result{Stdout: []byte("HTTP/2.0 200 OK\n\r\n[]")}, nil
	})
	must[[]github.Label](t)(c.RemoveLabel(1, "x:branch"))
	if want := "repos/user/project/issues/1/labels/x%3Abranch"; path != want {
		t.Fatalf("path %q, want %q", path, want)
	}
}
