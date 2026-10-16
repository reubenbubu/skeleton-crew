package ticket

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
)

// CommentWindow is how far back status reads comments.
const CommentWindow = 30 * 24 * time.Hour

// dwell is the expected dwell time of each stage, after which status flags
// a ticket at it as stale, and the flag.
var dwell = map[string]struct {
	after time.Duration
	flag  string
}{
	"1:intake":          {24 * time.Hour, "not triaged"},
	"2:drafting":        {7 * 24 * time.Hour, "not ready"},
	"3:eligible":        {24 * time.Hour, "not started"},
	"4:in-progress":     {4 * time.Hour, "not submitted"},
	"4:waiting-review":  {time.Hour, "no reviewer"},
	"4:review-rejected": {time.Hour, "no new attempt"},
	"5:review-approved": {time.Hour, "not merged"},
	"6:pending-merge":   {time.Hour, "not merged"},
	"7:merged":          {time.Hour, "no deployment"},
	"8:deploying":       {time.Hour, "deployment not finished"},
}

// pmStage reports whether the PM owns stage; the DM owns every other stage.
func pmStage(stage string) bool { return stage == "1:intake" || stage == "2:drafting" }

// Change is something status changed on ticket Number, such as
// "labelled stage=1:intake".
type Change struct {
	Number int    `json:"number"`
	Change string `json:"change"`
}

// Blocked is a blocked ticket, with the first line of its latest question
// that is not a Dev's.
type Blocked struct {
	Number   int       `json:"number"`
	Since    time.Time `json:"since"`
	Question string    `json:"question,omitempty"`
}

// Note is the first line of a comment on ticket Number.
type Note struct {
	Number int       `json:"number"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

// Stale is a ticket that has been at Stage since Since, longer than the
// stage's expected dwell time.
type Stale struct {
	Number int       `json:"number"`
	Stage  string    `json:"stage"`
	Since  time.Time `json:"since"`
	Flag   string    `json:"flag"`
}

// Proposal is the recovery action for a ticket.
type Proposal struct {
	Number int    `json:"number"`
	Stage  string `json:"stage"`
	Action string `json:"action"`
}

// Status is what needs a role's attention. Sections that do not concern the
// role are left empty.
type Status struct {
	Changes      []Change   `json:"changes,omitempty"`
	Waiting      []Blocked  `json:"waiting,omitempty"`
	Intake       []Summary  `json:"intake,omitempty"`
	Drafting     []Summary  `json:"drafting,omitempty"`
	Decisions    []Note     `json:"decisions,omitempty"`
	DevQuestions []Note     `json:"dev_questions,omitempty"`
	Board        []Summary  `json:"board,omitempty"`
	Stale        []Stale    `json:"stale,omitempty"`
	Recovery     []Proposal `json:"recovery,omitempty"`
}

// ReadStatus labels new issues 1:intake, closes finished parents, and
// returns what needs the attention of role, which is "pm", "dm" or "" for
// what concerns everyone.
func ReadStatus(c *github.Client, role string, now time.Time) (Status, error) {
	var s Status
	issues, err := c.Issues("open")
	if err != nil {
		return s, err
	}
	slices.SortFunc(issues, func(a, b github.Issue) int { return a.Number - b.Number })
	var open []github.Issue
	for _, i := range issues {
		if fieldsOf(i).Stage != "" {
			open = append(open, i)
			continue
		}
		ch, kept, err := tidy(c, i)
		if err != nil {
			return s, err
		}
		if ch != "" {
			s.Changes = append(s.Changes, Change{i.Number, ch})
		}
		if kept != nil {
			open = append(open, *kept)
		}
	}

	var blocked []github.Issue
	for _, i := range open {
		if fieldsOf(i).Blocked {
			blocked = append(blocked, i)
		}
	}
	var comments map[int][]Comment
	if role != "" || len(blocked) > 0 {
		since := now.Add(-CommentWindow)
		raw, err := c.RepoComments(since)
		if err != nil {
			return s, err
		}
		comments = map[int][]Comment{}
		for _, rc := range raw {
			// GitHub's since also returns older comments edited since.
			if rc.CreatedAt.Before(since) {
				continue
			}
			comments[rc.Issue()] = append(comments[rc.Issue()], parseComment(rc))
		}
	}

	for _, i := range blocked {
		since, err := entered(c, i, labels.Blocked)
		if err != nil {
			return s, err
		}
		b := Blocked{Number: i.Number, Since: since}
		for _, cm := range comments[i.Number] {
			// A Dev's questions are for the DM.
			if cm.Kind == "question" && cm.Role != "dev" {
				b.Question = firstLine(cm.Text)
			}
		}
		s.Waiting = append(s.Waiting, b)
	}

	unanswered := map[int]*Comment{}
	for _, i := range open {
		f := fieldsOf(i)
		sum := Summary{Number: i.Number, Title: i.Title, Fields: f}
		switch {
		case role == "pm" && f.Stage == "1:intake":
			s.Intake = append(s.Intake, sum)
		case role == "pm" && f.Stage == "2:drafting":
			s.Drafting = append(s.Drafting, sum)
		case role == "dm" && !pmStage(f.Stage):
			s.Board = append(s.Board, sum)
			var q *Comment
			for _, cm := range comments[i.Number] {
				switch {
				case cm.Kind == "question" && cm.Role == "dev":
					q = &cm
				case cm.Kind == "answer":
					q = nil
				}
			}
			if q != nil {
				unanswered[i.Number] = q
				s.DevQuestions = append(s.DevQuestions, Note{i.Number, q.At, firstLine(q.Text)})
			}
			if p := propose(f, q != nil); p != "" && !f.Blocked {
				s.Recovery = append(s.Recovery, Proposal{i.Number, f.Stage, p})
			}
		}
		d, ok := dwell[f.Stage]
		if !ok || f.Blocked || role == "" || pmStage(f.Stage) != (role == "pm") || !now.After(i.CreatedAt.Add(d.after)) {
			continue
		}
		since, err := entered(c, i, "stage:"+f.Stage)
		if err != nil {
			return s, err
		}
		if now.After(since.Add(d.after)) {
			s.Stale = append(s.Stale, Stale{i.Number, f.Stage, since, d.flag})
		}
	}

	if role == "pm" {
		numbers := make([]int, 0, len(comments))
		for n := range comments {
			numbers = append(numbers, n)
		}
		slices.Sort(numbers)
		for _, n := range numbers {
			var pending []Note
			for _, cm := range comments[n] {
				switch {
				case cm.Kind == "decision" && cm.Role == "dm":
					pending = append(pending, Note{n, cm.At, firstLine(cm.Text)})
				case cm.Kind == "log" && cm.Role == "pm" && strings.TrimSpace(cm.Text) == "ack":
					pending = nil
				}
			}
			s.Decisions = append(s.Decisions, pending...)
		}
	}
	return s, nil
}

// tidy handles an open issue without a stage. An issue with sub-issues is a
// parent: it is closed once they all are, and otherwise kept out of the
// views. Any other issue is labelled 1:intake, with the default priority and
// kind when it has none, and returned with its new labels.
func tidy(c *github.Client, i github.Issue) (string, *github.Issue, error) {
	subs, err := c.SubIssues(i.Number)
	if err != nil {
		return "", nil, err
	}
	if len(subs) > 0 {
		reason := "not_planned"
		for _, sub := range subs {
			if sub.State != "closed" {
				return "", nil, nil
			}
			if sub.StateReason != "not_planned" {
				reason = "completed"
			}
		}
		if _, err := c.EditIssue(i.Number, github.IssueEdit{State: "closed", StateReason: reason}); err != nil {
			return "", nil, err
		}
		return "closed: all sub-issues closed", nil, nil
	}
	f := fieldsOf(i)
	add := Fields{Stage: "1:intake"}
	if f.Priority == "" {
		add.Priority = "medium"
	}
	if f.Kind == "" {
		add.Kind = "feat"
	}
	names := []string{"stage:" + add.Stage}
	if add.Priority != "" {
		names = append(names, "priority:"+add.Priority)
	}
	if add.Kind != "" {
		names = append(names, "kind:"+add.Kind)
	}
	ls, err := c.AddLabels(i.Number, names...)
	if err != nil {
		return "", nil, err
	}
	i.Labels = ls
	return "labelled " + add.String(), &i, nil
}

// entered returns the last time label was added to issue i, or its creation
// time when its events do not say.
func entered(c *github.Client, i github.Issue, label string) (time.Time, error) {
	events, err := c.Events(i.Number)
	if err != nil {
		return time.Time{}, err
	}
	at := i.CreatedAt.UTC()
	for _, e := range events {
		if e.Event == "labeled" && e.Label != nil && e.Label.Name == label {
			at = e.CreatedAt.UTC()
		}
	}
	return at, nil
}

// propose returns the recovery action for a ticket with fields f, or "" for
// a stage that needs none.
func propose(f Fields, question bool) string {
	switch f.Stage {
	case "4:in-progress":
		dev := "start a Dev"
		if f.Tier != "" {
			dev += " at tier " + f.Tier
		}
		if question {
			return "answer the Dev question, then " + dev
		}
		return dev
	case "4:waiting-review":
		return "start a Reviewer"
	case "4:review-rejected":
		return "retry one tier up, then start a Dev"
	case "5:review-approved":
		return "merge"
	case "6:pending-merge":
		return "merge again"
	case "7:merged", "8:deploying":
		return "advance from the deployment status"
	}
	return ""
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	return first
}

// Next lists the eligible tickets that are not blocked and whose
// dependencies are closed as completed, by priority from critical to low,
// then oldest first.
func Next(c *github.Client) ([]Summary, error) {
	issues, err := c.Issues("open")
	if err != nil {
		return nil, err
	}
	var next []github.Issue
	for _, i := range issues {
		if f := fieldsOf(i); f.Stage != "3:eligible" || f.Blocked {
			continue
		}
		deps, err := c.BlockedBy(i.Number)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(deps, func(d github.Issue) bool { return !done(d) }) {
			next = append(next, i)
		}
	}
	rank := func(i github.Issue) int {
		r := slices.Index(Priorities, fieldsOf(i).Priority)
		if r < 0 {
			r = slices.Index(Priorities, "medium")
		}
		return -r
	}
	slices.SortFunc(next, func(a, b github.Issue) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), a.CreatedAt.Compare(b.CreatedAt), a.Number-b.Number)
	})
	out := []Summary{}
	for _, i := range next {
		out = append(out, Summary{Number: i.Number, Title: i.Title, Fields: fieldsOf(i)})
	}
	return out, nil
}

// done reports whether issue i is closed as completed.
func done(i github.Issue) bool {
	return i.State == "closed" && (i.StateReason == "completed" || i.StateReason == "")
}
