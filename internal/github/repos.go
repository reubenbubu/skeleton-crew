package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
)

// Repo is a GitHub repository.
type Repo struct {
	FullName string `json:"full_name"` // owner/name
	Private  bool   `json:"private"`
	CloneURL string `json:"clone_url"` // HTTPS
	SSHURL   string `json:"ssh_url"`
}

// User returns the account gh is logged in as. It needs no repository.
func (c *Client) User() (User, error) {
	var u User
	_, err := c.do(http.MethodGet, "user", nil, &u, http.StatusOK)
	return u, err
}

// CreateRepo creates the repository name under organisation org, or under
// the user's own account when org is "". It needs no repository.
func (c *Client) CreateRepo(org, name string, private bool) (Repo, error) {
	path := "user/repos"
	if org != "" {
		path = "orgs/" + url.PathEscape(org) + "/repos"
	}
	body := struct {
		Name    string `json:"name"`
		Private bool   `json:"private"`
	}{name, private}
	var r Repo
	_, err := c.do(http.MethodPost, path, body, &r, http.StatusCreated)
	return r, err
}

// NotFound reports whether err is GitHub answering 404 Not Found.
func NotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// AuthRequired reports whether err means gh is not logged in, or its login
// is no longer valid.
func AuthRequired(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	// gh exits with 4 when it has no login.
	return e.Status == http.StatusUnauthorized || (e.Status == 0 && e.Exit == 4)
}

// Version returns the version of gh, such as 2.46.0.
func Version(run Runner) (string, error) {
	out, err := local(run, "--version")
	if err != nil {
		return "", err
	}
	// gh version 2.46.0 (2024-03-26)
	f := strings.Fields(out)
	if len(f) < 3 || f[0] != "gh" || f[1] != "version" {
		return "", &Error{Detail: "unexpected output of gh --version"}
	}
	return f[2], nil
}

// GitProtocol returns the protocol gh is configured to use for git on
// github.com: ssh or https.
func GitProtocol(run Runner) (string, error) {
	out, err := local(run, "config", "get", "git_protocol", "--host", "github.com")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "ssh" {
		return "ssh", nil
	}
	return "https", nil
}

// local runs a gh command that reads only gh's own installation.
func local(run Runner, args ...string) (string, error) {
	res, err := run(args, nil)
	if errors.Is(err, exec.ErrNotFound) {
		return "", &Error{Detail: "not installed"}
	}
	if err != nil {
		return "", &Error{Detail: oneLine(err.Error())}
	}
	if res.Code != 0 {
		line, _, _ := strings.Cut(strings.TrimSpace(string(res.Stderr)), "\n")
		if line == "" {
			line = fmt.Sprintf("exit %d", res.Code)
		}
		return "", &Error{Exit: res.Code, Detail: oneLine("gh " + strings.Join(args, " ") + ": " + line)}
	}
	return string(res.Stdout), nil
}

var githubURL = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// RepoFromURL returns the owner/name of the github.com repository that a git
// remote URL points at, and false when the URL is not on github.com.
func RepoFromURL(url string) (string, bool) {
	m := githubURL.FindStringSubmatch(url)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Release is a published release of a repository.
type Release struct {
	TagName string `json:"tag_name"`
}

// LatestRelease returns the repository's latest release, which GitHub takes
// to be the newest one that is neither a draft nor a prerelease.
func (c *Client) LatestRelease() (Release, error) {
	var r Release
	_, err := c.do(http.MethodGet, c.path("releases/latest"), nil, &r, http.StatusOK)
	return r, err
}
