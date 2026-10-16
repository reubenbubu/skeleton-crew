// Package fakegithub is an in-memory GitHub for automated tests. Its Run
// method stands in for the gh executable: it takes the arguments that package
// github passes to `gh api`, serves the request in process and writes what gh
// would write. Nothing reaches the network.
//
// The fake answers with GitHub's JSON shapes, including fields crew ignores,
// and with GitHub's errors for the cases crew must handle.
package fakegithub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// Login is the account every request is made as.
const Login = "user"

// GitHub is one fake repository.
type GitHub struct {
	// Hook, when set, runs before each request. It may change state through
	// the GitHub's methods, for example to simulate another writer between a
	// read and a write. It may instead write its own response and return true
	// to replace the normal one.
	Hook func(w http.ResponseWriter, r *http.Request) bool

	repo  string
	clock clock.Clock
	mux   *http.ServeMux

	mu          sync.Mutex
	missing     bool // the repository does not exist yet
	public      bool
	loggedOut   bool
	gitProtocol string
	requests    []string
	nextID      int64
	issues      []*issue // issues and pull requests share numbers; index is number-1
	labels      []*label
	comments    map[int][]*comment
	events      map[int][]*event
	parent      map[int]int
	blockedBy   map[int][]int
	branches    map[string]string
	reviews     map[int][]*review
	statuses    map[string][]*status
	runs        []*run
}

type issue struct {
	id                 int64
	number             int
	title, body        string
	state, stateReason string
	labels             []string
	created            time.Time
	pull               *pull
}

type pull struct {
	head, base    string
	draft, merged bool
}

type label struct{ name, color, description string }

type comment struct {
	id      int64
	body    string
	created time.Time
}

// event is a label being added (labeled) or removed (unlabeled), or the
// issue being closed or reopened.
type event struct {
	id      int64
	event   string
	label   string // for labeled and unlabeled
	created time.Time
}

type review struct {
	id                    int64
	state, body, commitID string
	submitted             time.Time
}

type status struct {
	state, context, description string
	created                     time.Time
}

type run struct {
	id                                    int64
	name, branch, sha, status, conclusion string
	created                               time.Time
}

// New returns an empty fake repository, given as owner/name, whose
// timestamps come from c.
func New(repo string, c clock.Clock) *GitHub {
	g := &GitHub{
		repo:      repo,
		clock:     c,
		mux:       http.NewServeMux(),
		nextID:    1000,
		comments:  map[int][]*comment{},
		events:    map[int][]*event{},
		parent:    map[int]int{},
		blockedBy: map[int][]int{},
		branches:  map[string]string{},
		reviews:   map[int][]*review{},
		statuses:  map[string][]*status{},
	}
	p := "/repos/" + repo + "/"
	routes := map[string]http.HandlerFunc{
		"GET " + p + "issues":                              g.listIssues,
		"POST " + p + "issues":                             g.createIssue,
		"GET " + p + "issues/{n}":                          g.getIssue,
		"PATCH " + p + "issues/{n}":                        g.editIssue,
		"GET " + p + "labels":                              g.listLabels,
		"POST " + p + "labels":                             g.createLabel,
		"POST " + p + "issues/{n}/labels":                  g.addLabels,
		"DELETE " + p + "issues/{n}/labels/{name}":         g.removeLabel,
		"GET " + p + "issues/{n}/comments":                 g.listComments,
		"POST " + p + "issues/{n}/comments":                g.createComment,
		"GET " + p + "issues/{n}/events":                   g.listEvents,
		"GET " + p + "issues/{n}/sub_issues":               g.listSubIssues,
		"POST " + p + "issues/{n}/sub_issues":              g.addSubIssue,
		"GET " + p + "issues/{n}/dependencies/blocked_by":  g.listBlockedBy,
		"POST " + p + "issues/{n}/dependencies/blocked_by": g.addBlockedBy,
		"GET " + p + "pulls":                               g.listPulls,
		"POST " + p + "pulls":                              g.createPull,
		"GET " + p + "pulls/{n}":                           g.getPull,
		"GET " + p + "pulls/{n}/reviews":                   g.listReviews,
		"POST " + p + "pulls/{n}/reviews":                  g.createReview,
		"POST " + p + "statuses/{sha}":                     g.createStatus,
		"GET " + p + "commits/{sha}/status":                g.combinedStatus,
		"GET " + p + "actions/runs":                        g.listRuns,
	}
	routes["GET /user"] = g.getUser
	routes["POST /user/repos"] = g.createRepo
	routes["POST /orgs/{org}/repos"] = g.createRepo
	for pattern, h := range routes {
		g.mux.HandleFunc(pattern, h)
	}
	g.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "Not Found")
	})
	return g
}

// Run takes the place of github.Exec: it accepts only the `gh api` arguments
// that package github uses, and fails loudly on anything else.
func (g *GitHub) Run(args []string, stdin []byte) (github.Result, error) {
	usage := func(msg string) (github.Result, error) {
		return github.Result{Stderr: []byte(msg + "\n"), Code: 1}, nil
	}
	g.mu.Lock()
	loggedOut, protocol := g.loggedOut, g.gitProtocol
	g.mu.Unlock()
	switch {
	case slices.Equal(args, []string{"--version"}):
		return github.Result{Stdout: []byte("gh version " + Version + " (2024-03-26)\nhttps://github.com/cli/cli/releases/tag/v" + Version + "\n")}, nil
	case slices.Equal(args, []string{"config", "get", "git_protocol", "--host", "github.com"}):
		return github.Result{Stdout: []byte(protocol + "\n")}, nil
	case len(args) == 0 || args[0] != "api":
		return usage(fmt.Sprintf("fake gh: unsupported command %q", args))
	case loggedOut:
		// What gh prints, and its exit code, when it has no login.
		return github.Result{Stderr: []byte("To get started with GitHub CLI, please run:  gh auth login\n"), Code: 4}, nil
	}
	method, path, include, input := http.MethodGet, "", false, false
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--include" || a == "-i":
			include = true
		case (a == "--method" || a == "-X") && i+1 < len(args):
			i++
			method = args[i]
		case a == "--input" && i+1 < len(args) && args[i+1] == "-":
			i++
			input = true
		case strings.HasPrefix(a, "-") || path != "":
			return usage(fmt.Sprintf("fake gh: unsupported argument %q", a))
		default:
			path = a
		}
	}
	if path == "" {
		return usage("fake gh: no endpoint")
	}
	var body io.Reader = http.NoBody
	if input {
		body = bytes.NewReader(stdin)
	}
	req := httptest.NewRequest(method, "/"+strings.TrimPrefix(path, "/"), body)
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)

	var out bytes.Buffer
	if include {
		fmt.Fprintf(&out, "HTTP/2.0 %d %s\n", rec.Code, http.StatusText(rec.Code))
		names := make([]string, 0, len(rec.Header()))
		for n := range rec.Header() {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			fmt.Fprintf(&out, "%s: %s\r\n", n, strings.Join(rec.Header()[n], ", "))
		}
		out.WriteString("\r\n")
	}
	out.Write(rec.Body.Bytes())
	res := github.Result{Stdout: out.Bytes()}
	if rec.Code >= 400 {
		var e struct{ Message string }
		json.Unmarshal(rec.Body.Bytes(), &e)
		res.Stderr = []byte(fmt.Sprintf("gh: %s (HTTP %d)\n", e.Message, rec.Code))
		res.Code = 1
	}
	return res, nil
}

// ServeHTTP serves the GitHub REST API for this repository.
func (g *GitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r.Method+" "+r.URL.RequestURI())
	g.mu.Unlock()
	if g.Hook != nil && g.Hook(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Github-Request-Id", "FAKE:0001")
	g.mu.Lock()
	missing := g.missing
	g.mu.Unlock()
	if missing && strings.HasPrefix(r.URL.Path, "/repos/"+g.repo+"/") {
		fail(w, http.StatusNotFound, "Not Found")
		return
	}
	g.mux.ServeHTTP(w, r)
}

// Requests lists every request served so far, as "METHOD /path?query".
func (g *GitHub) Requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.requests)
}

// SetBranch makes branch name point at commit sha, as a push would. Pull
// requests from the branch follow it, and statuses can be set on sha.
func (g *GitHub) SetBranch(name, sha string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.branches[name] = sha
}

// IssueLabels returns the names of the labels on issue number.
func (g *GitHub) IssueLabels(number int) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i := g.issue(number); i != nil {
		return slices.Clone(i.labels)
	}
	return nil
}

// SetIssueLabels replaces the labels on issue number, as another writer
// would.
func (g *GitHub) SetIssueLabels(number int, names ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i := g.issue(number); i != nil {
		for _, l := range slices.Clone(i.labels) {
			if !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(l, n) }) {
				g.removeIssueLabel(i, l)
			}
		}
		g.addIssueLabels(i, names)
	}
}

// AddWorkflowRun records a workflow run on commit sha of branch.
func (g *GitHub) AddWorkflowRun(name, branch, sha, status, conclusion string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = append(g.runs, &run{id: g.id(), name: name, branch: branch, sha: sha, status: status, conclusion: conclusion, created: g.clock.Now()})
}

// knownCommit reports whether a branch points at sha.
func (g *GitHub) knownCommit(sha string) bool {
	for _, s := range g.branches {
		if s == sha {
			return true
		}
	}
	return false
}

func (g *GitHub) id() int64 {
	g.nextID++
	return g.nextID
}

func (g *GitHub) issue(number int) *issue {
	if number < 1 || number > len(g.issues) {
		return nil
	}
	return g.issues[number-1]
}

func (g *GitHub) issueByID(id int64) *issue {
	for _, i := range g.issues {
		if i.id == id {
			return i
		}
	}
	return nil
}

// pathIssue returns the issue named by the {n} path value, or writes a 404.
func (g *GitHub) pathIssue(w http.ResponseWriter, r *http.Request) *issue {
	n, err := strconv.Atoi(r.PathValue("n"))
	if i := g.issue(n); err == nil && i != nil {
		return i
	}
	fail(w, http.StatusNotFound, "Not Found")
	return nil
}

func (g *GitHub) pathPull(w http.ResponseWriter, r *http.Request) *issue {
	n, err := strconv.Atoi(r.PathValue("n"))
	if i := g.issue(n); err == nil && i != nil && i.pull != nil {
		return i
	}
	fail(w, http.StatusNotFound, "Not Found")
	return nil
}

func (g *GitHub) addIssueLabels(i *issue, names []string) {
	for _, n := range names {
		if g.label(n) == nil {
			g.labels = append(g.labels, &label{name: n, color: "ededed"})
		}
		if !slices.ContainsFunc(i.labels, func(l string) bool { return strings.EqualFold(l, n) }) {
			i.labels = append(i.labels, g.label(n).name)
			g.record(i, "labeled", g.label(n).name)
		}
	}
}

// removeIssueLabel removes label name from i and reports whether i had it.
func (g *GitHub) removeIssueLabel(i *issue, name string) bool {
	k := slices.IndexFunc(i.labels, func(l string) bool { return strings.EqualFold(l, name) })
	if k < 0 {
		return false
	}
	g.record(i, "unlabeled", i.labels[k])
	i.labels = slices.Delete(i.labels, k, k+1)
	return true
}

func (g *GitHub) record(i *issue, kind, label string) {
	g.events[i.number] = append(g.events[i.number], &event{id: g.id(), event: kind, label: label, created: g.clock.Now()})
}

func (g *GitHub) label(name string) *label {
	for _, l := range g.labels {
		if strings.EqualFold(l.name, name) {
			return l
		}
	}
	return nil
}

func (g *GitHub) url(format string, a ...any) string {
	return "https://api.github.com/repos/" + g.repo + "/" + fmt.Sprintf(format, a...)
}

func user() map[string]any {
	return map[string]any{"login": Login, "id": 1, "node_id": "U_kgDOfake", "type": "User"}
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (g *GitHub) labelJSON(l *label) map[string]any {
	return map[string]any{"id": 1, "node_id": "LA_kwDOfake", "url": g.url("labels/%s", l.name), "name": l.name, "color": l.color, "description": l.description, "default": false}
}

func (g *GitHub) labelsJSON(names []string) []any {
	out := []any{}
	for _, n := range names {
		out = append(out, g.labelJSON(g.label(n)))
	}
	return out
}

func (g *GitHub) issueJSON(i *issue) map[string]any {
	m := map[string]any{
		"id": i.id, "node_id": fmt.Sprintf("I_kwDOfake%d", i.id), "number": i.number,
		"url": g.url("issues/%d", i.number), "title": i.title, "body": nullable(i.body),
		"state": i.state, "state_reason": nullable(i.stateReason), "labels": g.labelsJSON(i.labels),
		"user": user(), "comments": len(g.comments[i.number]), "created_at": ts(i.created),
		"reactions": map[string]any{"total_count": 0},
	}
	if i.pull != nil {
		m["pull_request"] = map[string]any{"url": g.url("pulls/%d", i.number)}
	}
	return m
}

func (g *GitHub) pullJSON(i *issue) map[string]any {
	owner, _, _ := strings.Cut(g.repo, "/")
	return map[string]any{
		"id": i.id, "node_id": fmt.Sprintf("PR_kwDOfake%d", i.id), "number": i.number,
		"url": g.url("pulls/%d", i.number), "title": i.title, "body": nullable(i.body),
		"state": i.state, "draft": i.pull.draft, "merged": i.pull.merged, "user": user(),
		"head":       map[string]any{"ref": i.pull.head, "sha": g.branches[i.pull.head], "label": owner + ":" + i.pull.head},
		"base":       map[string]any{"ref": i.pull.base, "sha": g.branches[i.pull.base], "label": owner + ":" + i.pull.base},
		"created_at": ts(i.created),
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func reply(w http.ResponseWriter, code int, v any) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string, errs ...any) {
	m := map[string]any{"message": msg, "documentation_url": "https://docs.github.com/rest", "status": strconv.Itoa(code)}
	if len(errs) > 0 {
		m["errors"] = errs
	}
	reply(w, code, m)
}

func invalid(w http.ResponseWriter, resource, field, code string) {
	fail(w, http.StatusUnprocessableEntity, "Validation Failed", map[string]any{"resource": resource, "field": field, "code": code})
}

// decode reads the JSON request body into v, or writes a 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "Problems parsing JSON")
		return false
	}
	return true
}

// page returns the slice of items that the per_page and page parameters
// select, with GitHub's defaults.
func page[T any](r *http.Request, items []T) []T {
	q := r.URL.Query()
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per <= 0 {
		per = 30
	}
	per = min(per, 100)
	p, _ := strconv.Atoi(q.Get("page"))
	p = max(p, 1)
	start := min((p-1)*per, len(items))
	return items[start:min(start+per, len(items))]
}

func (g *GitHub) listIssues(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	out := []any{}
	for _, i := range slices.Backward(g.issues) {
		if state == "all" || i.state == state {
			out = append(out, g.issueJSON(i))
		}
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) createIssue(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var in struct {
		Title  string
		Body   string
		Labels []string
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Title == "" {
		invalid(w, "Issue", "title", "missing_field")
		return
	}
	i := &issue{id: g.id(), number: len(g.issues) + 1, title: in.Title, body: in.Body, state: "open", created: g.clock.Now()}
	g.issues = append(g.issues, i)
	g.addIssueLabels(i, in.Labels)
	reply(w, http.StatusCreated, g.issueJSON(i))
}

func (g *GitHub) getIssue(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i := g.pathIssue(w, r); i != nil {
		reply(w, http.StatusOK, g.issueJSON(i))
	}
}

func (g *GitHub) editIssue(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	var in struct {
		Title, Body *string
		State       string
		StateReason string `json:"state_reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.State != "" && in.State != "open" && in.State != "closed" {
		invalid(w, "Issue", "state", "invalid")
		return
	}
	if in.Title != nil {
		i.title = *in.Title
	}
	if in.Body != nil {
		i.body = *in.Body
	}
	if in.State != "" && in.State != i.state {
		g.record(i, map[string]string{"open": "reopened", "closed": "closed"}[in.State], "")
	}
	if in.State != "" {
		i.state = in.State
		i.stateReason = "reopened"
		if in.State == "closed" {
			i.stateReason = "completed"
		}
	}
	if in.StateReason != "" {
		i.stateReason = in.StateReason
	}
	reply(w, http.StatusOK, g.issueJSON(i))
}

func (g *GitHub) listLabels(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []any{}
	for _, l := range g.labels {
		out = append(out, g.labelJSON(l))
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) createLabel(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var in struct{ Name, Color, Description string }
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" {
		invalid(w, "Label", "name", "missing_field")
		return
	}
	if g.label(in.Name) != nil {
		invalid(w, "Label", "name", "already_exists")
		return
	}
	if in.Color == "" {
		in.Color = "ededed"
	}
	l := &label{name: in.Name, color: in.Color, description: in.Description}
	g.labels = append(g.labels, l)
	reply(w, http.StatusCreated, g.labelJSON(l))
}

func (g *GitHub) addLabels(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	var in struct{ Labels []string }
	if !decode(w, r, &in) {
		return
	}
	g.addIssueLabels(i, in.Labels)
	reply(w, http.StatusOK, g.labelsJSON(i.labels))
}

func (g *GitHub) removeLabel(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	if !g.removeIssueLabel(i, r.PathValue("name")) {
		fail(w, http.StatusNotFound, "Label does not exist")
		return
	}
	reply(w, http.StatusOK, g.labelsJSON(i.labels))
}

func (g *GitHub) commentJSON(number int, c *comment) map[string]any {
	return map[string]any{
		"id": c.id, "node_id": fmt.Sprintf("IC_kwDOfake%d", c.id), "url": g.url("issues/comments/%d", c.id),
		"issue_url": g.url("issues/%d", number), "body": c.body, "user": user(),
		"created_at": ts(c.created), "updated_at": ts(c.created), "author_association": "OWNER",
	}
}

func (g *GitHub) listComments(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	out := []any{}
	for _, c := range g.comments[i.number] {
		out = append(out, g.commentJSON(i.number, c))
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) createComment(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	var in struct{ Body string }
	if !decode(w, r, &in) {
		return
	}
	if in.Body == "" {
		invalid(w, "IssueComment", "body", "missing_field")
		return
	}
	c := &comment{id: g.id(), body: in.Body, created: g.clock.Now()}
	g.comments[i.number] = append(g.comments[i.number], c)
	reply(w, http.StatusCreated, g.commentJSON(i.number, c))
}

func (g *GitHub) listEvents(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	out := []any{}
	for _, e := range g.events[i.number] {
		m := map[string]any{
			"id": e.id, "node_id": fmt.Sprintf("LE_kwDOfake%d", e.id), "url": g.url("issues/events/%d", e.id),
			"actor": user(), "event": e.event, "commit_id": nil, "commit_url": nil,
			"created_at": ts(e.created), "performed_via_github_app": nil,
		}
		if e.label != "" {
			l := g.label(e.label)
			m["label"] = map[string]any{"name": l.name, "color": l.color}
		}
		out = append(out, m)
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) listSubIssues(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.pathIssue(w, r)
	if p == nil {
		return
	}
	out := []any{}
	for _, i := range g.issues {
		if g.parent[i.number] == p.number {
			out = append(out, g.issueJSON(i))
		}
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) addSubIssue(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.pathIssue(w, r)
	if p == nil {
		return
	}
	var in struct {
		SubIssueID int64 `json:"sub_issue_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	c := g.issueByID(in.SubIssueID)
	switch {
	case c == nil:
		fail(w, http.StatusNotFound, "Not Found")
	case c == p || g.parent[c.number] != 0:
		fail(w, http.StatusUnprocessableEntity, "Validation Failed", map[string]any{"resource": "SubIssue", "code": "custom", "message": "Issue may not contain duplicate sub-issues and Sub issue may only have one parent"})
	default:
		g.parent[c.number] = p.number
		reply(w, http.StatusCreated, g.issueJSON(c))
	}
}

func (g *GitHub) listBlockedBy(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	out := []any{}
	for _, n := range g.blockedBy[i.number] {
		out = append(out, g.issueJSON(g.issue(n)))
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) addBlockedBy(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathIssue(w, r)
	if i == nil {
		return
	}
	var in struct {
		IssueID int64 `json:"issue_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	b := g.issueByID(in.IssueID)
	switch {
	case b == nil:
		fail(w, http.StatusNotFound, "Not Found")
	case b == i || slices.Contains(g.blockedBy[i.number], b.number):
		invalid(w, "IssueDependency", "issue_id", "invalid")
	default:
		g.blockedBy[i.number] = append(g.blockedBy[i.number], b.number)
		reply(w, http.StatusCreated, g.issueJSON(b))
	}
}

func (g *GitHub) listPulls(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" {
		state = "open"
	}
	owner, _, _ := strings.Cut(g.repo, "/")
	out := []any{}
	for _, i := range slices.Backward(g.issues) {
		if i.pull == nil || (state != "all" && i.state != state) {
			continue
		}
		if h := q.Get("head"); h != "" && h != owner+":"+i.pull.head {
			continue
		}
		out = append(out, g.pullJSON(i))
	}
	reply(w, http.StatusOK, page(r, out))
}

func (g *GitHub) createPull(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var in struct {
		Title, Body, Head, Base string
		Draft                   bool
	}
	if !decode(w, r, &in) {
		return
	}
	_, ok := g.branches[in.Head]
	switch {
	case in.Title == "":
		invalid(w, "PullRequest", "title", "missing_field")
		return
	case !ok:
		invalid(w, "PullRequest", "head", "invalid")
		return
	case g.branches[in.Base] == "":
		invalid(w, "PullRequest", "base", "invalid")
		return
	}
	for _, i := range g.issues {
		if i.pull != nil && i.state == "open" && i.pull.head == in.Head && i.pull.base == in.Base {
			fail(w, http.StatusUnprocessableEntity, "Validation Failed", map[string]any{"resource": "PullRequest", "code": "custom", "message": "A pull request already exists for " + in.Head + "."})
			return
		}
	}
	i := &issue{id: g.id(), number: len(g.issues) + 1, title: in.Title, body: in.Body, state: "open", created: g.clock.Now(),
		pull: &pull{head: in.Head, base: in.Base, draft: in.Draft}}
	g.issues = append(g.issues, i)
	reply(w, http.StatusCreated, g.pullJSON(i))
}

func (g *GitHub) getPull(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if i := g.pathPull(w, r); i != nil {
		reply(w, http.StatusOK, g.pullJSON(i))
	}
}

func (g *GitHub) reviewJSON(number int, rv *review) map[string]any {
	return map[string]any{
		"id": rv.id, "node_id": fmt.Sprintf("PRR_kwDOfake%d", rv.id), "user": user(), "body": rv.body,
		"state": rv.state, "commit_id": rv.commitID, "submitted_at": ts(rv.submitted),
		"pull_request_url": g.url("pulls/%d", number), "author_association": "OWNER",
	}
}

func (g *GitHub) listReviews(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathPull(w, r)
	if i == nil {
		return
	}
	out := []any{}
	for _, rv := range g.reviews[i.number] {
		out = append(out, g.reviewJSON(i.number, rv))
	}
	reply(w, http.StatusOK, page(r, out))
}

// createReview refuses an approval or a change request, because every pull
// request in the fake is the requester's own, as it is on GitHub when every
// agent uses the user's gh login.
func (g *GitHub) createReview(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.pathPull(w, r)
	if i == nil {
		return
	}
	var in struct {
		Event, Body string
		CommitID    string `json:"commit_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	var state string
	switch in.Event {
	case "APPROVE":
		fail(w, http.StatusUnprocessableEntity, "Unprocessable Entity", "Review Can not approve your own pull request")
		return
	case "REQUEST_CHANGES":
		fail(w, http.StatusUnprocessableEntity, "Unprocessable Entity", "Review Can not request changes on your own pull request")
		return
	case "COMMENT":
		if in.Body == "" {
			fail(w, http.StatusUnprocessableEntity, "Unprocessable Entity", "Review body is required for COMMENT")
			return
		}
		state = "COMMENTED"
	default:
		invalid(w, "PullRequestReview", "event", "invalid")
		return
	}
	if in.CommitID == "" {
		in.CommitID = g.branches[i.pull.head]
	}
	rv := &review{id: g.id(), state: state, body: in.Body, commitID: in.CommitID, submitted: g.clock.Now()}
	g.reviews[i.number] = append(g.reviews[i.number], rv)
	reply(w, http.StatusOK, g.reviewJSON(i.number, rv))
}

func statusJSON(s *status) map[string]any {
	return map[string]any{
		"id": 1, "node_id": "SC_kwDOfake", "state": s.state, "context": s.context,
		"description": nullable(s.description), "target_url": nil,
		"created_at": ts(s.created), "updated_at": ts(s.created), "creator": user(),
	}
}

func (g *GitHub) createStatus(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var in struct{ State, Context, Description string }
	if !decode(w, r, &in) {
		return
	}
	if !slices.Contains([]string{"error", "failure", "pending", "success"}, in.State) {
		invalid(w, "Status", "state", "custom")
		return
	}
	if in.Context == "" {
		in.Context = "default"
	}
	sha := r.PathValue("sha")
	if !g.knownCommit(sha) {
		fail(w, http.StatusUnprocessableEntity, "No commit found for SHA: "+sha)
		return
	}
	s := &status{state: in.State, context: in.Context, description: in.Description, created: g.clock.Now()}
	g.statuses[sha] = append(g.statuses[sha], s)
	reply(w, http.StatusCreated, statusJSON(s))
}

func (g *GitHub) combinedStatus(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sha := r.PathValue("sha")
	if !g.knownCommit(sha) {
		fail(w, http.StatusUnprocessableEntity, "No commit found for SHA: "+sha)
		return
	}
	latest := map[string]*status{}
	var order []string
	for _, s := range g.statuses[sha] {
		if _, ok := latest[s.context]; !ok {
			order = append(order, s.context)
		}
		latest[s.context] = s
	}
	state, out := "success", []any{}
	if len(order) == 0 {
		state = "pending"
	}
	for _, c := range order {
		s := latest[c]
		out = append(out, statusJSON(s))
		switch {
		case s.state == "error" || s.state == "failure":
			state = "failure"
		case s.state == "pending" && state != "failure":
			state = "pending"
		}
	}
	reply(w, http.StatusOK, map[string]any{"state": state, "statuses": out, "sha": sha, "total_count": len(out)})
}

func (g *GitHub) listRuns(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	q := r.URL.Query()
	out := []any{}
	for _, run := range slices.Backward(g.runs) {
		if (q.Get("head_sha") != "" && q.Get("head_sha") != run.sha) || (q.Get("branch") != "" && q.Get("branch") != run.branch) {
			continue
		}
		out = append(out, map[string]any{
			"id": run.id, "node_id": "WFR_kwDOfake", "name": run.name, "head_branch": run.branch,
			"head_sha": run.sha, "event": "push", "status": run.status, "conclusion": nullable(run.conclusion),
			"html_url":   "https://github.com/" + g.repo + "/actions/runs/" + strconv.FormatInt(run.id, 10),
			"created_at": ts(run.created),
		})
	}
	reply(w, http.StatusOK, map[string]any{"total_count": len(out), "workflow_runs": page(r, out)})
}

// Version is the gh version the fake reports.
const Version = "2.46.0"

// SetExists sets whether the repository exists. A new fake's repository
// exists; until it does, its endpoints answer 404 and it can be created.
func (g *GitHub) SetExists(exists bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.missing = !exists
}

func (g *GitHub) Exists() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.missing
}

// Private reports whether the repository is private.
func (g *GitHub) Private() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.public
}

// SetLoggedIn sets whether gh has a login. Without one, every API call fails
// before any HTTP request, as gh does.
func (g *GitHub) SetLoggedIn(in bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loggedOut = !in
}

// SetGitProtocol sets what gh's git_protocol setting holds: "", https or ssh.
func (g *GitHub) SetGitProtocol(p string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gitProtocol = p
}

func (g *GitHub) getUser(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, user())
}

// createRepo serves both POST /user/repos and POST /orgs/{org}/repos. Only
// the fake's own repository can be created.
func (g *GitHub) createRepo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Private bool   `json:"private"`
	}
	if !decode(w, r, &in) {
		return
	}
	owner := r.PathValue("org")
	if owner == "" {
		owner = Login
	}
	full := owner + "/" + in.Name
	g.mu.Lock()
	defer g.mu.Unlock()
	if full != g.repo {
		fail(w, http.StatusUnprocessableEntity, "fake github: only "+g.repo+" can be created")
		return
	}
	if !g.missing {
		fail(w, http.StatusUnprocessableEntity, "Repository creation failed.",
			map[string]any{"resource": "Repository", "code": "custom", "field": "name", "message": "name already exists on this account"})
		return
	}
	g.missing, g.public = false, !in.Private
	reply(w, http.StatusCreated, map[string]any{
		"id": g.id(), "node_id": "R_kgDOfake", "name": in.Name, "full_name": full, "private": in.Private,
		"owner":    map[string]any{"login": owner, "id": 1, "type": "User"},
		"html_url": "https://github.com/" + full, "url": "https://api.github.com/repos/" + full,
		"clone_url": "https://github.com/" + full + ".git", "ssh_url": "git@github.com:" + full + ".git",
		"default_branch": "main", "created_at": ts(g.clock.Now()),
	})
}
