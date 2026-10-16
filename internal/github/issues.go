package github

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Issue is a GitHub issue. ID is the API's own identifier, which sub-issues
// and dependencies need; it never appears in crew's output.
type Issue struct {
	ID          int64     `json:"id"`
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	StateReason string    `json:"state_reason"`
	Labels      []Label   `json:"labels"`
	User        User      `json:"user"`
	CreatedAt   time.Time `json:"created_at"`
	PullRequest *struct{} `json:"pull_request"` // set when the issue is a pull request
}

type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

type User struct {
	Login string `json:"login"`
}

type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	IssueURL  string    `json:"issue_url"`
}

// Issue returns the number of the issue or pull request the comment is on,
// or 0 when its URL does not say.
func (c Comment) Issue() int {
	_, n, _ := strings.Cut(c.IssueURL, "/issues/")
	number, err := strconv.Atoi(n)
	if err != nil {
		return 0
	}
	return number
}

type NewIssue struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
}

// IssueEdit changes the fields that are set.
type IssueEdit struct {
	Title       *string `json:"title,omitempty"`
	Body        *string `json:"body,omitempty"`
	State       string  `json:"state,omitempty"`        // open or closed
	StateReason string  `json:"state_reason,omitempty"` // completed or not_planned
}

func (c *Client) CreateIssue(n NewIssue) (Issue, error) {
	var i Issue
	_, err := c.do(http.MethodPost, c.path("issues"), n, &i, http.StatusCreated)
	return i, err
}

func (c *Client) Issue(number int) (Issue, error) {
	var i Issue
	_, err := c.do(http.MethodGet, c.path("issues/%d", number), nil, &i, http.StatusOK)
	return i, err
}

// Issues lists the issues in state open, closed or all, leaving out pull
// requests.
func (c *Client) Issues(state string) ([]Issue, error) {
	all, err := list[Issue](c, c.path("issues?state=%s", url.QueryEscape(state)))
	if err != nil {
		return nil, err
	}
	issues := all[:0]
	for _, i := range all {
		if i.PullRequest == nil {
			issues = append(issues, i)
		}
	}
	return issues, nil
}

func (c *Client) EditIssue(number int, e IssueEdit) (Issue, error) {
	var i Issue
	_, err := c.do(http.MethodPatch, c.path("issues/%d", number), e, &i, http.StatusOK)
	return i, err
}

// Labels lists the repository's labels.
func (c *Client) Labels() ([]Label, error) {
	return list[Label](c, c.path("labels"))
}

func (c *Client) CreateLabel(l Label) error {
	_, err := c.do(http.MethodPost, c.path("labels"), l, nil, http.StatusCreated)
	return err
}

// AddLabels adds labels to an issue and returns all its labels.
func (c *Client) AddLabels(number int, names ...string) ([]Label, error) {
	var ls []Label
	body := struct {
		Labels []string `json:"labels"`
	}{names}
	_, err := c.do(http.MethodPost, c.path("issues/%d/labels", number), body, &ls, http.StatusOK)
	return ls, err
}

// RemoveLabel removes a label from an issue and returns the labels left.
func (c *Client) RemoveLabel(number int, name string) ([]Label, error) {
	var ls []Label
	_, err := c.do(http.MethodDelete, c.labelPath(number, name), nil, &ls, http.StatusOK)
	return ls, err
}

func (c *Client) labelPath(number int, name string) string {
	// gh would expand a name such as x:branch as a placeholder.
	escaped := strings.ReplaceAll(url.PathEscape(name), ":", "%3A")
	return c.path("issues/%d/labels/%s", number, escaped)
}

func (c *Client) Comments(number int) ([]Comment, error) {
	return list[Comment](c, c.path("issues/%d/comments", number))
}

// RepoComments lists the comments on every issue and pull request of the
// repository that were updated at or after since, oldest first.
func (c *Client) RepoComments(since time.Time) ([]Comment, error) {
	return list[Comment](c, c.path("issues/comments?since=%s", url.QueryEscape(since.UTC().Format(time.RFC3339))))
}

func (c *Client) CreateComment(number int, body string) (Comment, error) {
	var cm Comment
	b := struct {
		Body string `json:"body"`
	}{body}
	_, err := c.do(http.MethodPost, c.path("issues/%d/comments", number), b, &cm, http.StatusCreated)
	return cm, err
}

func (c *Client) SubIssues(parent int) ([]Issue, error) {
	return list[Issue](c, c.path("issues/%d/sub_issues", parent))
}

// AddSubIssue makes the issue with API ID childID a sub-issue of parent.
func (c *Client) AddSubIssue(parent int, childID int64) error {
	b := struct {
		SubIssueID int64 `json:"sub_issue_id"`
	}{childID}
	_, err := c.do(http.MethodPost, c.path("issues/%d/sub_issues", parent), b, nil, http.StatusCreated)
	return err
}

// Parent returns the issue that issue number is a sub-issue of, and false
// when it has none.
func (c *Client) Parent(number int) (Issue, bool, error) {
	var i Issue
	status, err := c.do(http.MethodGet, c.path("issues/%d/parent", number), nil, &i, http.StatusOK, http.StatusNotFound)
	return i, err == nil && status == http.StatusOK, err
}

// BlockedBy lists the issues that issue number is blocked by.
func (c *Client) BlockedBy(number int) ([]Issue, error) {
	return list[Issue](c, c.path("issues/%d/dependencies/blocked_by", number))
}

// AddBlockedBy records that issue number is blocked by the issue with API ID
// blockerID.
func (c *Client) AddBlockedBy(number int, blockerID int64) error {
	b := struct {
		IssueID int64 `json:"issue_id"`
	}{blockerID}
	_, err := c.do(http.MethodPost, c.path("issues/%d/dependencies/blocked_by", number), b, nil, http.StatusCreated)
	return err
}

// RemoveBlockedBy removes the record that issue number is blocked by the
// issue with API ID blockerID.
func (c *Client) RemoveBlockedBy(number int, blockerID int64) error {
	_, err := c.do(http.MethodDelete, c.path("issues/%d/dependencies/blocked_by/%d", number, blockerID), nil, nil, http.StatusOK)
	return err
}

// Event is one entry in an issue's event history, such as a label being
// added (labeled) or removed (unlabeled), or the issue being closed.
type Event struct {
	Event     string    `json:"event"`
	Label     *Label    `json:"label"` // set for labeled and unlabeled
	CreatedAt time.Time `json:"created_at"`
}

// Events lists the events of issue number, oldest first.
func (c *Client) Events(number int) ([]Event, error) {
	return list[Event](c, c.path("issues/%d/events", number))
}

// TakeLabel removes a label from an issue and reports whether this call
// removed it. Of several calls for the same label, only one gets true.
func (c *Client) TakeLabel(number int, name string) (bool, error) {
	status, err := c.do(http.MethodDelete, c.labelPath(number, name), nil, nil, http.StatusOK, http.StatusNotFound)
	return status == http.StatusOK, err
}
