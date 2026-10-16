package ticket

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

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
