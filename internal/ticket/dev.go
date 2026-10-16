package ticket

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a ticket's title into the last part of its branch name: lower
// case, every run of other characters than letters and digits turned into
// -, and cut to at most 40 characters at a -. A title with nothing left
// gives "ticket".
func Slug(title string) string {
	s := strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 40 {
		if i := strings.LastIndex(s[:41], "-"); i > 0 {
			s = s[:i]
		} else {
			s = s[:40]
		}
	}
	if s == "" {
		return "ticket"
	}
	return s
}

// Branch is the name of ticket number's branch, such as
// feat/12/login-with-email.
func Branch(kind string, number int, title string) string {
	return fmt.Sprintf("%s/%d/%s", kind, number, Slug(title))
}

var branchName = regexp.MustCompile(`^(feat|fix|chore)/([1-9][0-9]*)/[a-z0-9-]+$`)

// ParseBranch returns the kind and number of the ticket that branch belongs
// to, and false when it is not a ticket branch.
func ParseBranch(branch string) (kind string, number int, ok bool) {
	m := branchName.FindStringSubmatch(branch)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	return m[1], n, err == nil
}

// Eligible reads ticket number, which must be at 3:eligible and have a
// kind.
func Eligible(c *github.Client, number int) (github.Issue, Fields, error) {
	i, f, err := get(c, number)
	if err != nil {
		return i, f, err
	}
	if f.Stage != "3:eligible" {
		return i, f, stageChanged(number, f.Stage, "3:eligible")
	}
	if !slices.Contains(Kinds, f.Kind) {
		return i, f, fmt.Errorf("#%d has no single kind: kind=%s", number, f.Kind)
	}
	return i, f, nil
}

// InFlight lists the open tickets at a stage 4, which each hold one of the
// parallel slots, in number order.
func InFlight(c *github.Client) ([]int, error) {
	tickets, err := List(c)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, t := range tickets {
		for s := range strings.SplitSeq(t.Stage, ",") {
			if strings.HasPrefix(s, "4:") {
				out = append(out, t.Number)
				break
			}
		}
	}
	return out, nil
}

// Start moves ticket number from 3:eligible to 4:in-progress and makes rank
// its tier. The tier goes on first, so a failure leaves the ticket at 3:eligible,
// where start can run again.
func Start(c *github.Client, number, rank int) error {
	tier := fmt.Sprintf("tier:%d", rank)
	if _, err := c.AddLabels(number, tier); err != nil {
		return err
	}
	i, err := move(c, number, "3:eligible", "4:in-progress", nil)
	if err != nil {
		return err
	}
	for _, l := range i.Labels {
		if strings.HasPrefix(l.Name, "tier:") && l.Name != tier {
			if _, err := c.TakeLabel(number, l.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Stuck moves ticket number from 4:in-progress to 4:review-rejected, a
// failed attempt, and posts the Dev's reason as a log comment.
func Stuck(c *github.Client, number int, reason string) error {
	if _, err := move(c, number, "4:in-progress", "4:review-rejected", nil); err != nil {
		return err
	}
	_, err := c.CreateComment(number, Marker("log", "dev")+"stuck: "+reason)
	return err
}

// LogRed records a red result on ticket number: the criteria named in the
// lines the commit added to test files, the test files, and the end of the
// test output. It names no commit, so it stays valid across a rebase.
func LogRed(c *github.Client, number int, criteria, files []string, excerpt string) error {
	first := "red: no criterion named"
	if len(criteria) > 0 {
		first = "red: " + strings.Join(criteria, " ")
	}
	body := Marker("log", "dev") + first + "\ntests: " + strings.Join(files, " ") + "\n```\n" + excerpt + "```\n"
	_, err := c.CreateComment(number, body)
	return err
}

// OpenPR returns the open pull request from branch for ticket number, and
// opens one if there is none. opened reports whether it did.
func OpenPR(c *github.Client, number int, kind, branch string) (pr int, opened bool, err error) {
	prs, err := c.PullRequests(branch, "open")
	if err != nil || len(prs) > 0 {
		if len(prs) > 0 {
			pr = prs[0].Number
		}
		return pr, false, err
	}
	i, _, err := get(c, number)
	if err != nil {
		return 0, false, err
	}
	p, err := c.CreatePullRequest(github.NewPullRequest{
		Title: fmt.Sprintf("[%s:#%d] %s", kind, number, i.Title),
		Body:  fmt.Sprintf("Ticket #%d", number),
		Head:  branch,
		Base:  "main",
	})
	return p.Number, err == nil, err
}

// Known is what the gate needs to know of a ticket that a test names: its
// criteria, each with the time it was last revised, or the zero time, and
// whether it was cancelled.
type Known struct {
	Criteria  map[string]time.Time
	Cancelled bool
}

var criterionLine = regexp.MustCompile(`(?m)^\s*[-*] \[(#([0-9]+)-AC[0-9]+)\]\s+(?:\(revised (\S+)\))?`)

// Criteria returns the acceptance criteria of ticket number, from its body,
// each with the time a (revised <time>) mark gives it, or the zero time.
func Criteria(number int, body string) map[string]time.Time {
	out := map[string]time.Time{}
	for _, m := range criterionLine.FindAllStringSubmatch(sections(body)["Acceptance criteria"], -1) {
		if m[2] != strconv.Itoa(number) {
			continue
		}
		at, _ := time.Parse("2006-01-02T15:04Z", m[3])
		out[m[1]] = at
	}
	return out
}

// Index reads every ticket, open or closed, as the gate needs to know it.
func Index(c *github.Client) (map[int]Known, error) {
	issues, err := c.Issues("all")
	if err != nil {
		return nil, err
	}
	out := map[int]Known{}
	for _, i := range issues {
		if i.PullRequest != nil {
			continue
		}
		out[i.Number] = Known{
			Criteria:  Criteria(i.Number, i.Body),
			Cancelled: fieldsOf(i).Stage == "0:cancelled" || i.StateReason == "not_planned",
		}
	}
	return out, nil
}

var redIDs = regexp.MustCompile(`#[0-9]+-AC[0-9]+`)

// Red returns the criteria that the red log comments on ticket number name.
func Red(c *github.Client, number int) ([]string, error) {
	raw, err := c.Comments(number)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rc := range raw {
		cm := parseComment(rc)
		first, _, _ := strings.Cut(strings.ReplaceAll(cm.Text, "\r\n", "\n"), "\n")
		if cm.Kind != "log" || cm.Role != "dev" || !strings.HasPrefix(first, "red: ") {
			continue
		}
		for _, id := range redIDs.FindAllString(first, -1) {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// InProgress reads ticket number, which must be at 4:in-progress.
func InProgress(c *github.Client, number int) (github.Issue, error) {
	i, f, err := get(c, number)
	if err == nil && f.Stage != "4:in-progress" {
		err = stageChanged(number, f.Stage, "4:in-progress")
	}
	return i, err
}

// Submit moves ticket number from 4:in-progress to 4:waiting-review, once
// the gate has passed.
func Submit(c *github.Client, number int) error {
	_, err := move(c, number, "4:in-progress", "4:waiting-review", nil)
	return err
}

// Review posts the Reviewer's verdict on ticket number, which must be at
// 4:waiting-review, and moves it to 5:review-approved, or to
// 4:review-rejected when reason is not "". GitHub refuses an approval or a
// change request on one's own pull request, and every agent uses the user's
// login, so the verdict is a comment review of the PR's head commit. It is
// also a review comment on the ticket, for crew ticket show. Review returns
// the PR's number.
func Review(c *github.Client, number int, reason string) (int, error) {
	i, f, err := get(c, number)
	if err != nil {
		return 0, err
	}
	if f.Stage != "4:waiting-review" {
		return 0, stageChanged(number, f.Stage, "4:waiting-review")
	}
	branch := Branch(f.Kind, number, i.Title)
	prs, err := c.PullRequests(branch, "open")
	if err != nil {
		return 0, err
	}
	if len(prs) == 0 {
		return 0, fmt.Errorf("#%d has no open PR from %s", number, branch)
	}
	pr := prs[0]
	body, to := Marker("review", "reviewer")+"approved", "5:review-approved"
	if reason != "" {
		body, to = Marker("review", "reviewer")+"rejected: "+reason, "4:review-rejected"
	}
	if _, err := c.CreateReview(pr.Number, github.NewReview{Event: "COMMENT", Body: body, CommitID: pr.Head.SHA}); err != nil {
		return pr.Number, err
	}
	if _, err := c.CreateComment(number, body); err != nil {
		return pr.Number, err
	}
	_, err = move(c, number, "4:waiting-review", to, nil)
	return pr.Number, err
}

// PendingMerge moves ticket number from 5:review-approved to
// 6:pending-merge and returns it. A ticket already at 6:pending-merge, whose
// merge stopped before, stays there.
func PendingMerge(c *github.Client, number int) (github.Issue, Fields, error) {
	i, f, err := get(c, number)
	if err != nil {
		return i, f, err
	}
	if !slices.Contains(Kinds, f.Kind) {
		return i, f, fmt.Errorf("#%d has no single kind: kind=%s", number, f.Kind)
	}
	if f.Stage != "6:pending-merge" {
		_, err = move(c, number, "5:review-approved", "6:pending-merge", nil)
	}
	return i, f, err
}

// Merged moves ticket number from 6:pending-merge to 7:merged. Without a
// deployment workflow it goes on to 9:done and is closed. Merged returns
// the stage reached.
func Merged(c *github.Client, number int, deploy bool) (string, error) {
	if _, err := move(c, number, "6:pending-merge", "7:merged", nil); err != nil || deploy {
		return "7:merged", err
	}
	if _, err := move(c, number, "7:merged", "9:done", nil); err != nil {
		return "7:merged", err
	}
	_, err := c.EditIssue(number, github.IssueEdit{State: "closed", StateReason: "completed"})
	return "9:done", err
}
