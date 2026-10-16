// Package github is the only way crew talks to GitHub. Every read and write
// is one `gh api` call; no other package runs gh or calls the API. Version
// and GitProtocol also run gh, but only read its local installation.
//
// A Client stops at the first unexpected response: every later call returns
// the same error without running gh, so a command that hits one makes no
// further change.
package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Client reads and writes one repository through gh.
type Client struct {
	repo string // owner/name
	run  Runner
	err  error
}

// New returns a Client for repo, given as owner/name, that runs gh with run.
func New(repo string, run Runner) *Client {
	return &Client{repo: repo, run: run}
}

// Error is an unexpected GitHub response, or a failure of gh itself. Its
// message is one terse line.
type Error struct {
	Method, Path string // Path leaves out the query
	Status       int    // 0 when gh failed before any HTTP response
	Exit         int    // gh's exit code when it failed before any HTTP response
	Detail       string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return "gh failed: " + e.Detail
	}
	s := fmt.Sprintf("unexpected github response: %s %s: HTTP %d", e.Method, e.Path, e.Status)
	if e.Detail != "" {
		s += " " + e.Detail
	}
	return s
}

// do runs one `gh api` call. It sends body as JSON when it is not nil and
// decodes the response into out when out is not nil. A status outside want
// is an Error. It returns the status.
func (c *Client) do(method, path string, body, out any, want ...int) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	status, err := c.call(method, path, body, out, want)
	if err != nil {
		c.err = err
	}
	return status, err
}

func (c *Client) call(method, path string, body, out any, want []int) (int, error) {
	short, _, _ := strings.Cut(path, "?")
	fail := func(status int, detail string) (int, error) {
		return status, &Error{Method: method, Path: short, Status: status, Detail: detail}
	}
	args := []string{"api", "--include", "--method", method, path}
	var stdin []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		args = append(args, "--input", "-")
		stdin = b
	}
	res, err := c.run(args, stdin)
	if err != nil {
		return fail(0, oneLine(err.Error()))
	}
	status, resp, ok := parseResponse(res.Stdout)
	if !ok {
		line, _, _ := strings.Cut(strings.TrimSpace(string(res.Stderr)), "\n")
		if line == "" {
			line = fmt.Sprintf("exit %d with no HTTP response", res.Code)
		}
		return 0, &Error{Method: method, Path: short, Exit: res.Code, Detail: oneLine(line)}
	}
	if !slices.Contains(want, status) {
		return fail(status, detail(resp))
	}
	if out != nil && json.Unmarshal(resp, out) != nil {
		return fail(status, "invalid JSON")
	}
	return status, nil
}

// parseResponse splits the output of `gh api --include` into the HTTP status
// and the body. gh ends the status line with \n and the headers with \r\n.
func parseResponse(b []byte) (status int, body []byte, ok bool) {
	line, rest, found := bytes.Cut(b, []byte("\n"))
	f := strings.Fields(string(line))
	if !found || len(f) < 2 || !strings.HasPrefix(f[0], "HTTP/") {
		return 0, nil, false
	}
	status, err := strconv.Atoi(f[1])
	if err != nil {
		return 0, nil, false
	}
	for {
		line, rest, found = bytes.Cut(rest, []byte("\n"))
		if len(bytes.TrimSuffix(line, []byte("\r"))) == 0 {
			return status, rest, found || len(line) == 0
		}
		if !found {
			return 0, nil, false
		}
	}
}

// detail summarises an error response: GitHub's message and the reasons it
// lists, or else the start of the body.
func detail(body []byte) string {
	var e struct {
		Message string            `json:"message"`
		Errors  []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil || e.Message == "" {
		return oneLine(strings.ToValidUTF8(string(body[:min(len(body), 200)]), ""))
	}
	var reasons []string
	for _, raw := range e.Errors {
		var s string
		var o struct{ Message, Field, Code string }
		switch {
		case json.Unmarshal(raw, &s) == nil:
			reasons = append(reasons, s)
		case json.Unmarshal(raw, &o) == nil && o.Message != "":
			reasons = append(reasons, o.Message)
		case o.Field != "" && o.Code != "":
			reasons = append(reasons, o.Field+": "+o.Code)
		case o.Code != "":
			reasons = append(reasons, o.Code)
		}
	}
	if len(reasons) > 0 {
		return oneLine(e.Message + " (" + strings.Join(reasons, "; ") + ")")
	}
	return oneLine(e.Message)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// list reads every page of a list endpoint.
func list[T any](c *Client, path string) ([]T, error) {
	const per = 100
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	var all []T
	for page := 1; ; page++ {
		var batch []T
		if _, err := c.do("GET", fmt.Sprintf("%s%sper_page=%d&page=%d", path, sep, per, page), nil, &batch, 200); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < per {
			return all, nil
		}
	}
}

func (c *Client) path(format string, a ...any) string {
	return "repos/" + c.repo + "/" + fmt.Sprintf(format, a...)
}
