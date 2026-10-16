// Package ticket reads and writes tickets, which are GitHub issues with
// crew's labels, and builds the terse views of them.
package ticket

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
)

// Skeleton is the body of a new ticket.
const Skeleton = "## Goal\n\n## Acceptance criteria\n\n## Out of scope\n\n## Assumptions\n\n## Non-functional\n\n## Risks\n"

var (
	Priorities   = []string{"low", "medium", "high", "critical"}
	Kinds        = []string{"feat", "fix", "chore"}
	CommentKinds = []string{"decision", "question", "answer", "review", "log"}
	Roles        = []string{"pm", "dm", "dev", "reviewer"}
)

// Human is the kind of a comment without crew's marker.
const Human = "human"

// Fields are a ticket's labels as values. Stage leaves out the stage:
// prefix, such as 4:in-progress; it is "" for a ticket with no stage. A
// ticket with two labels for one field, which is outside the normal path,
// has both values joined by a comma, such as 2:drafting,3:eligible, so the
// views show it and no stage move accepts it.
type Fields struct {
	Stage    string `json:"stage,omitempty"`
	Priority string `json:"priority,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Tier     string `json:"tier,omitempty"`
	Testing  string `json:"testing,omitempty"`
	Blocked  bool   `json:"blocked"`
}

// String formats the fields as in crew's views, such as
// "stage=4:in-progress priority=high kind=feat tier=2 blocked".
func (f Fields) String() string {
	var s []string
	add := func(k, v string) {
		if v != "" {
			s = append(s, k+"="+v)
		}
	}
	add("stage", f.Stage)
	add("priority", f.Priority)
	add("kind", f.Kind)
	add("tier", f.Tier)
	add("testing", f.Testing)
	if f.Blocked {
		s = append(s, labels.Blocked)
	}
	return strings.Join(s, " ")
}

func fieldsOf(i github.Issue) Fields {
	var f Fields
	set := func(field *string, v string) {
		if *field != "" {
			v = *field + "," + v
		}
		*field = v
	}
	for _, l := range i.Labels {
		key, v, _ := strings.Cut(l.Name, ":")
		switch key {
		case "stage":
			set(&f.Stage, v)
		case "priority":
			set(&f.Priority, v)
		case "kind":
			set(&f.Kind, v)
		case "tier":
			set(&f.Tier, v)
		case "testing":
			set(&f.Testing, v)
		case labels.Blocked:
			f.Blocked = true
		}
	}
	return f
}

// get reads ticket number and its fields. A pull request or a missing issue
// is an error that says so.
func get(c *github.Client, number int) (github.Issue, Fields, error) {
	i, err := c.Issue(number)
	if github.NotFound(err) {
		return i, Fields{}, fmt.Errorf("#%d does not exist", number)
	}
	if err != nil {
		return i, Fields{}, err
	}
	if i.PullRequest != nil {
		return i, Fields{}, fmt.Errorf("#%d is a pull request, not a ticket", number)
	}
	return i, fieldsOf(i), nil
}

// Create creates a ticket with the skeleton body at 2:drafting, or at
// 1:intake when intake is true. A parent other than 0 makes it a sub-issue
// of that ticket, which then keeps no stage. The parent must be open and at
// 1:intake, 2:drafting or no stage.
func Create(c *github.Client, title, priority, kind string, intake bool, parent int) (int, Fields, error) {
	var pf Fields
	if parent != 0 {
		p, f, err := get(c, parent)
		if err != nil {
			return 0, Fields{}, err
		}
		if p.State == "closed" {
			return 0, Fields{}, fmt.Errorf("#%d is closed", parent)
		}
		if f.Stage != "" && !pmStage(f.Stage) {
			return 0, Fields{}, fmt.Errorf("#%d is at %s; a parent must be at 1:intake or 2:drafting", parent, f.Stage)
		}
		pf = f
	}
	i, f, err := create(c, title, priority, kind, intake)
	if err != nil || parent == 0 {
		return i.Number, f, err
	}
	if err := c.AddSubIssue(parent, i.ID); err != nil {
		return i.Number, f, err
	}
	for s := range strings.SplitSeq(pf.Stage, ",") {
		if s == "" {
			continue
		}
		if _, err := c.TakeLabel(parent, "stage:"+s); err != nil {
			return i.Number, f, err
		}
	}
	return i.Number, f, nil
}

func create(c *github.Client, title, priority, kind string, intake bool) (github.Issue, Fields, error) {
	f := Fields{Stage: "2:drafting", Priority: priority, Kind: kind}
	if intake {
		f.Stage = "1:intake"
	}
	i, err := c.CreateIssue(github.NewIssue{
		Title:  title,
		Body:   Skeleton,
		Labels: []string{"stage:" + f.Stage, "priority:" + priority, "kind:" + kind},
	})
	return i, f, err
}

// Triage moves ticket number from 1:intake to 2:drafting.
func Triage(c *github.Client, number int) error {
	_, err := move(c, number, "1:intake", "2:drafting", nil)
	return err
}

// Ready moves ticket number from 2:drafting to 3:eligible, once its goal
// and acceptance criteria are filled in.
func Ready(c *github.Client, number int) error {
	_, err := move(c, number, "2:drafting", "3:eligible", func(i github.Issue) error {
		return checkReady(number, i.Body)
	})
	return err
}

// move changes the stage of ticket number from to to, after check, if any,
// accepts the issue, and only if the ticket is still at from. Each caller
// allows only a forward move.
func move(c *github.Client, number int, from, to string, check func(github.Issue) error) (github.Issue, error) {
	if to != Cancelled && stageRank(to) < stageRank(from) {
		panic("backward stage move: " + from + " to " + to)
	}
	i, f, err := get(c, number)
	if err != nil {
		return i, err
	}
	if f.Stage != from {
		return i, stageChanged(number, f.Stage, from)
	}
	if check != nil {
		if err := check(i); err != nil {
			return i, err
		}
	}
	// GitHub has no conditional label write, so the removal of the old stage
	// is the check-and-set: of two moves that race, only one removes it. The
	// new stage goes on first, so a failure in between leaves two stages,
	// which crew reports, never none.
	if _, err := c.AddLabels(number, "stage:"+to); err != nil {
		return i, err
	}
	if took, err := c.TakeLabel(number, "stage:"+from); took || err != nil {
		return i, err
	}
	// Another writer moved the ticket since the read. Unless it made the
	// same move, the new stage is this move's alone and comes off again.
	i, f, err = get(c, number)
	if err != nil {
		return i, err
	}
	stages := strings.Split(f.Stage, ",")
	if len(stages) > 1 && slices.Contains(stages, to) {
		if _, err := c.TakeLabel(number, "stage:"+to); err != nil {
			return i, err
		}
		stages = slices.DeleteFunc(stages, func(s string) bool { return s == to })
	}
	return i, stageChanged(number, strings.Join(stages, ","), from)
}

// stageChanged refuses a move from stage from of a ticket that is at stage.
func stageChanged(number int, stage, from string) error {
	if stage == "" {
		return fmt.Errorf("stage-changed: #%d has no stage, not %s", number, from)
	}
	return fmt.Errorf("stage-changed: #%d is at %s, not %s", number, stage, from)
}

// stageRank returns the number of a stage such as 4:in-progress.
func stageRank(stage string) int {
	n, _ := strconv.Atoi(stage[:strings.Index(stage, ":")])
	return n
}

var section = regexp.MustCompile(`(?m)^## +(.*?)\s*$`)

// sections splits a body into its "## " sections, by heading.
func sections(body string) map[string]string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	out := map[string]string{}
	locs := section.FindAllStringSubmatchIndex(body, -1)
	for k, loc := range locs {
		end := len(body)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		out[body[loc[2]:loc[3]]] = body[loc[1]:end]
	}
	return out
}

func checkReady(number int, body string) error {
	s := sections(body)
	if strings.TrimSpace(s["Goal"]) == "" {
		return fmt.Errorf("#%d not ready: the goal is empty", number)
	}
	criterion := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*[-*] \[#%d-AC\d+\] +\S`, number))
	if !criterion.MatchString(s["Acceptance criteria"]) {
		return fmt.Errorf("#%d not ready: no acceptance criterion such as - [#%d-AC1] <text>", number, number)
	}
	return nil
}

// Comment is a ticket comment. Kind is Human, and Role is "", for a comment
// without crew's marker.
type Comment struct {
	Kind   string    `json:"kind"`
	Role   string    `json:"role,omitempty"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

var marker = regexp.MustCompile(`^<!-- crew:([a-z]+) ([a-z]+) -->\r?\n`)

// Marker returns the invisible line that starts a comment of kind by role.
func Marker(kind, role string) string {
	return "<!-- crew:" + kind + " " + role + " -->\n"
}

func parseComment(c github.Comment) Comment {
	cm := Comment{Kind: Human, Author: c.User.Login, At: c.CreatedAt.UTC(), Text: c.Body}
	if m := marker.FindStringSubmatch(c.Body); m != nil && slices.Contains(CommentKinds, m[1]) && slices.Contains(Roles, m[2]) {
		cm.Kind, cm.Role, cm.Text = m[1], m[2], c.Body[len(m[0]):]
	}
	return cm
}

// Post adds a comment of kind by role to ticket number.
func Post(c *github.Client, number int, kind, role, text string) error {
	if _, _, err := get(c, number); err != nil {
		return err
	}
	_, err := c.CreateComment(number, Marker(kind, role)+text)
	return err
}

// Entry is one line of a ticket's timeline: a stage reached, blocked set or
// cleared, the issue closed or reopened, or a log comment, whose first line
// is Text.
type Entry struct {
	At    time.Time `json:"at"`
	Event string    `json:"event"`
	Text  string    `json:"text,omitempty"`
}

// View is the terse view of a ticket.
type View struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Fields
	Body     string    `json:"body"`
	Timeline []Entry   `json:"timeline"`
	Comments []Comment `json:"comments"`
}

// Show reads the view of ticket number. Unless all is true, log comments
// are collapsed into the timeline.
func Show(c *github.Client, number int, all bool) (View, error) {
	i, f, err := get(c, number)
	if err != nil {
		return View{}, err
	}
	events, err := c.Events(number)
	if err != nil {
		return View{}, err
	}
	raw, err := c.Comments(number)
	if err != nil {
		return View{}, err
	}
	v := View{Number: number, Title: i.Title, Fields: f, Body: i.Body, Timeline: []Entry{}, Comments: []Comment{}}
	for _, e := range events {
		if (e.Event == "labeled" || e.Event == "unlabeled") && e.Label == nil {
			continue
		}
		var name string
		switch {
		case e.Event == "labeled" && strings.HasPrefix(e.Label.Name, "stage:"):
			_, name, _ = strings.Cut(strings.TrimPrefix(e.Label.Name, "stage:"), ":")
		case e.Event == "labeled" && e.Label.Name == labels.Blocked:
			name = "blocked"
		case e.Event == "unlabeled" && e.Label.Name == labels.Blocked:
			name = "unblocked"
		case e.Event == "closed" || e.Event == "reopened":
			name = e.Event
		default:
			continue
		}
		v.Timeline = append(v.Timeline, Entry{At: e.CreatedAt.UTC(), Event: name})
	}
	for _, rc := range raw {
		cm := parseComment(rc)
		if cm.Kind == "log" && !all {
			first, _, _ := strings.Cut(strings.TrimSpace(strings.ReplaceAll(cm.Text, "\r\n", "\n")), "\n")
			v.Timeline = append(v.Timeline, Entry{At: cm.At, Event: "log", Text: first})
			continue
		}
		v.Comments = append(v.Comments, cm)
	}
	slices.SortStableFunc(v.Timeline, func(a, b Entry) int { return a.At.Compare(b.At) })
	return v, nil
}

// Summary is one ticket in a list.
type Summary struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Fields
}

// List reads the open tickets in number order.
func List(c *github.Client) ([]Summary, error) {
	issues, err := c.Issues("open")
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, i := range issues {
		out = append(out, Summary{Number: i.Number, Title: i.Title, Fields: fieldsOf(i)})
	}
	slices.SortFunc(out, func(a, b Summary) int { return a.Number - b.Number })
	return out, nil
}
