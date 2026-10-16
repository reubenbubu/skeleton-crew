package ticket

import (
	"fmt"
	"slices"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// DeployWorkflows are the paths of a deployment workflow.
var DeployWorkflows = []string{".github/workflows/deploy.yml", ".github/workflows/deploy.yaml"}

// deployment returns the commit that merged ticket i, the head of its
// merged pull request, or "" when it has none, and the newest run of the
// deployment workflow on that commit, or nil when there is none yet.
func deployment(c *github.Client, i github.Issue, f Fields) (string, *github.WorkflowRun, error) {
	prs, err := c.PullRequests(Branch(f.Kind, i.Number, i.Title), "closed")
	if err != nil {
		return "", nil, err
	}
	m := slices.IndexFunc(prs, func(p github.PullRequest) bool { return p.MergedAt != nil })
	if m < 0 {
		return "", nil, nil
	}
	sha := prs[m].Head.SHA
	runs, err := c.WorkflowRuns(sha)
	if err != nil {
		return sha, nil, err
	}
	var newest *github.WorkflowRun
	for _, r := range runs {
		if slices.Contains(DeployWorkflows, r.Path) && (newest == nil || r.CreatedAt.After(newest.CreatedAt)) {
			newest = &r
		}
	}
	return sha, newest, nil
}

// failed reports whether run finished without success.
func failed(run *github.WorkflowRun) bool {
	return run != nil && run.Status == "completed" && run.Conclusion != "success"
}

// advance moves ticket i, at 7:merged or 8:deploying with fields f, on
// from the status of its deployment: to 8:deploying once the deployment
// workflow has run on its merged commit, and to 9:done, closed as
// completed, once that run succeeded. It returns the change made, if any,
// the stage reached, and what is left for the DM, if anything.
func advance(c *github.Client, i github.Issue, f Fields) (change, stage, action string, err error) {
	stage = f.Stage
	sha, run, err := deployment(c, i, f)
	if err != nil {
		return "", stage, "", err
	}
	if sha == "" {
		return "", stage, "find the merged commit: no merged PR from " + Branch(f.Kind, i.Number, i.Title), nil
	}
	if run == nil {
		return "", stage, "wait for the deployment", nil
	}
	if stage == "7:merged" {
		if _, err := move(c, i.Number, stage, "8:deploying", nil); err != nil {
			return "", stage, "", err
		}
		stage, change = "8:deploying", "moved to 8:deploying"
	}
	switch {
	case run.Status != "completed":
		return change, stage, "wait for the deployment", nil
	case run.Conclusion != "success":
		return change, stage, "triage the deployment: " + run.Conclusion, nil
	}
	if _, err := move(c, i.Number, stage, "9:done", nil); err != nil {
		return change, stage, "", err
	}
	if _, err := c.EditIssue(i.Number, github.IssueEdit{State: "closed", StateReason: "completed"}); err != nil {
		return change, "9:done", "", err
	}
	return "deployed: moved to 9:done and closed", "9:done", "", nil
}

// Fix handles a deployment of ticket number that failed because of its
// code. The ticket must be open at 7:merged or 8:deploying, and the newest
// run of the deployment workflow on its merged commit must have finished
// without success. Fix opens a fix ticket called title at priority high and
// 2:drafting, links the two with a log comment on each, and moves ticket
// number to 9:done and closes it. It returns the new ticket.
func Fix(c *github.Client, number int, title string) (Summary, error) {
	i, f, err := get(c, number)
	if err != nil {
		return Summary{}, err
	}
	if i.State == "closed" {
		return Summary{}, fmt.Errorf("#%d is closed", number)
	}
	if f.Stage != "7:merged" && f.Stage != "8:deploying" {
		return Summary{}, fmt.Errorf("stage-changed: #%d is at %s, not 7:merged or 8:deploying", number, f.Stage)
	}
	_, run, err := deployment(c, i, f)
	if err != nil {
		return Summary{}, err
	}
	if !failed(run) {
		return Summary{}, fmt.Errorf("#%d has no failed deployment", number)
	}
	fix, ff, err := create(c, title, "high", "fix", false)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Number: fix.Number, Title: title, Fields: ff}
	if err := Post(c, fix.Number, "log", "dm", fmt.Sprintf("fix for the deployment of #%d", number)); err != nil {
		return s, err
	}
	if err := Post(c, number, "log", "dm", fmt.Sprintf("deployment failed: fix in #%d", fix.Number)); err != nil {
		return s, err
	}
	stage := f.Stage
	if stage == "7:merged" {
		if _, err := move(c, number, stage, "8:deploying", nil); err != nil {
			return s, err
		}
		stage = "8:deploying"
	}
	if _, err := move(c, number, stage, "9:done", nil); err != nil {
		return s, err
	}
	_, err = c.EditIssue(number, github.IssueEdit{State: "closed", StateReason: "completed"})
	return s, err
}
