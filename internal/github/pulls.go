package github

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	Draft     bool       `json:"draft"`
	Merged    bool       `json:"merged"` // only filled by PullRequest, not PullRequests
	MergedAt  *time.Time `json:"merged_at"`
	Head      Ref        `json:"head"`
	Base      Ref        `json:"base"`
	User      User       `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
}

type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type NewPullRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

type Review struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"` // APPROVED, CHANGES_REQUESTED or COMMENTED
	Body        string    `json:"body"`
	User        User      `json:"user"`
	CommitID    string    `json:"commit_id"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type NewReview struct {
	Event    string `json:"event"` // APPROVE, REQUEST_CHANGES or COMMENT
	Body     string `json:"body"`
	CommitID string `json:"commit_id,omitempty"`
}

// Status is a commit status, such as crew/gate.
type Status struct {
	State       string    `json:"state"` // error, failure, pending or success
	Context     string    `json:"context"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at,omitzero"`
}

// CombinedStatus is the latest status of each context on a commit.
type CombinedStatus struct {
	State    string   `json:"state"`
	Statuses []Status `json:"statuses"`
}

type WorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"` // the workflow file, such as .github/workflows/deploy.yml
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HeadBranch string    `json:"head_branch"`
	HeadSHA    string    `json:"head_sha"`
	Event      string    `json:"event"`
	CreatedAt  time.Time `json:"created_at"`
}

func (c *Client) CreatePullRequest(n NewPullRequest) (PullRequest, error) {
	var p PullRequest
	_, err := c.do(http.MethodPost, c.path("pulls"), n, &p, http.StatusCreated)
	return p, err
}

func (c *Client) PullRequest(number int) (PullRequest, error) {
	var p PullRequest
	_, err := c.do(http.MethodGet, c.path("pulls/%d", number), nil, &p, http.StatusOK)
	return p, err
}

// PullRequests lists the pull requests from branch head in state open,
// closed or all.
func (c *Client) PullRequests(head, state string) ([]PullRequest, error) {
	owner, _, _ := strings.Cut(c.repo, "/")
	return list[PullRequest](c, c.path("pulls?head=%s&state=%s", url.QueryEscape(owner+":"+head), url.QueryEscape(state)))
}

func (c *Client) CreateReview(number int, r NewReview) (Review, error) {
	var rv Review
	_, err := c.do(http.MethodPost, c.path("pulls/%d/reviews", number), r, &rv, http.StatusOK)
	return rv, err
}

func (c *Client) Reviews(number int) ([]Review, error) {
	return list[Review](c, c.path("pulls/%d/reviews", number))
}

func (c *Client) CreateStatus(sha string, s Status) error {
	_, err := c.do(http.MethodPost, c.path("statuses/%s", url.PathEscape(sha)), s, nil, http.StatusCreated)
	return err
}

func (c *Client) CombinedStatus(sha string) (CombinedStatus, error) {
	var s CombinedStatus
	_, err := c.do(http.MethodGet, c.path("commits/%s/status?per_page=100", url.PathEscape(sha)), nil, &s, http.StatusOK)
	return s, err
}

// WorkflowRuns lists the most recent workflow runs, up to 100, on commit sha.
func (c *Client) WorkflowRuns(sha string) ([]WorkflowRun, error) {
	var r struct {
		Runs []WorkflowRun `json:"workflow_runs"`
	}
	_, err := c.do(http.MethodGet, c.path("actions/runs?head_sha=%s&per_page=100", url.QueryEscape(sha)), nil, &r, http.StatusOK)
	return r.Runs, err
}
