package ticket

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
)

// Cancelled is the stage of a ticket that will not be done. It is the one
// stage a ticket may reach from any open stage.
const Cancelled = "0:cancelled"

// markLayout is the layout of the time in a criterion's (revised <time>)
// mark.
const markLayout = "2006-01-02T15:04Z"

// Retry starts the next attempt on ticket number, which must be at
// 4:review-rejected and not blocked: it moves the ticket to 4:in-progress
// one rank up, to at most maxTier, and returns the rank. When the attempt
// would be one more than topAttempts at maxTier since blocked was last
// cleared, it starts nothing: it sets blocked and posts a question for the
// user that lists the attempts and their failure reasons.
func Retry(c *github.Client, number, maxTier, topAttempts int) (int, error) {
	_, f, err := get(c, number)
	if err != nil {
		return 0, err
	}
	if f.Stage != "4:review-rejected" {
		return 0, stageChanged(number, f.Stage, "4:review-rejected")
	}
	if f.Blocked {
		return 0, errors.New(blocked(number))
	}
	tier, err := strconv.Atoi(f.Tier)
	if err != nil {
		return 0, fmt.Errorf("#%d has no single tier: tier=%s", number, f.Tier)
	}
	rank := min(tier+1, maxTier)
	events, err := c.Events(number)
	if err != nil {
		return 0, err
	}
	tiers, since := attempts(events)
	top := 0
	for _, t := range tiers[since:] {
		if t == strconv.Itoa(maxTier) {
			top++
		}
	}
	if rank < maxTier || top < topAttempts {
		return rank, attempt(c, number, "4:review-rejected", rank)
	}
	reasons, err := failures(c, number)
	if err != nil {
		return 0, err
	}
	var tried []string
	for _, t := range tiers {
		tried = append(tried, "tier "+t)
	}
	q := fmt.Sprintf("#%d failed %d attempts at tier %d, the max_tier. How should it go on: rethink the approach, split it, or drop it?\n", number, top, maxTier) +
		"attempts: " + strings.Join(tried, ", ") + "\n"
	for _, r := range reasons {
		q += r + "\n"
	}
	// The question goes first: a ticket blocked without it would show the
	// user nothing, and a retry would refuse to ask again.
	if _, err := c.CreateComment(number, Marker("question", "dm")+q); err != nil {
		return 0, err
	}
	if _, err := c.AddLabels(number, labels.Blocked); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("escalation-limit: #%d had %d attempts at tier %d, the max_tier; blocked with a question for the user", number, top, maxTier)
}

// attempts reads the attempts of a ticket from its events: the tier of each
// time it reached 4:in-progress, except from 6:pending-merge, which is a
// repair, and the index of the first attempt since blocked was last
// cleared.
func attempts(events []github.Event) (tiers []string, since int) {
	var tier, stage string
	for _, e := range events {
		if e.Label == nil {
			continue
		}
		switch {
		case e.Event == "labeled" && strings.HasPrefix(e.Label.Name, "tier:"):
			tier = strings.TrimPrefix(e.Label.Name, "tier:")
		case e.Event == "labeled" && e.Label.Name == "stage:4:in-progress":
			if stage != "stage:6:pending-merge" {
				tiers = append(tiers, tier)
			}
			stage = e.Label.Name
		case e.Event == "labeled" && strings.HasPrefix(e.Label.Name, "stage:"):
			stage = e.Label.Name
		case e.Event == "unlabeled" && e.Label.Name == labels.Blocked:
			since = len(tiers)
		}
	}
	return tiers, since
}

// failures returns the first line of each Reviewer's rejection and each
// Dev's stuck report on ticket number, in order.
func failures(c *github.Client, number int) ([]string, error) {
	raw, err := c.Comments(number)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rc := range raw {
		cm := parseComment(rc)
		first := firstLine(cm.Text)
		if (cm.Kind == "review" && cm.Role == "reviewer" && strings.HasPrefix(first, "rejected: ")) ||
			(cm.Kind == "log" && cm.Role == "dev" && strings.HasPrefix(first, "stuck: ")) {
			out = append(out, first)
		}
	}
	return out, nil
}

// Appeal moves ticket number from 4:review-rejected back to
// 4:waiting-review for a second Reviewer, and posts the DM's reason as a
// review comment. The current attempt must have ended with a Reviewer's
// rejection and must not have been appealed before.
func Appeal(c *github.Client, number int, reason string) error {
	_, f, err := get(c, number)
	if err != nil {
		return err
	}
	if f.Stage != "4:review-rejected" {
		return stageChanged(number, f.Stage, "4:review-rejected")
	}
	events, err := c.Events(number)
	if err != nil {
		return err
	}
	reviews := 0
	for _, e := range events {
		switch {
		case e.Event != "labeled" || e.Label == nil:
		case e.Label.Name == "stage:4:in-progress":
			reviews = 0
		case e.Label.Name == "stage:4:waiting-review":
			reviews++
		}
	}
	if reviews == 0 {
		return fmt.Errorf("#%d has no review rejection to appeal in this attempt", number)
	}
	if reviews > 1 {
		return fmt.Errorf("#%d had its one appeal in this attempt", number)
	}
	if _, err := move(c, number, "4:review-rejected", "4:waiting-review", nil); err != nil {
		return err
	}
	_, err = c.CreateComment(number, Marker("review", "dm")+"appeal: "+reason)
	return err
}

// openStage checks that ticket number, issue i with fields f, is open at one
// stage that is not 9:done.
func openStage(number int, i github.Issue, f Fields) error {
	if i.State == "closed" {
		return fmt.Errorf("#%d is closed", number)
	}
	if f.Stage == "" {
		return fmt.Errorf("#%d has no stage", number)
	}
	if f.Stage == "9:done" || !slices.ContainsFunc(labels.Stages, func(s labels.Stage) bool { return s.Label == "stage:"+f.Stage }) {
		return fmt.Errorf("stage-changed: #%d is at %s, not an open stage", number, f.Stage)
	}
	return nil
}

// Cancel moves ticket number from any open stage to 0:cancelled and closes
// it as not planned. A ticket already at 0:cancelled but still open is
// closed.
func Cancel(c *github.Client, number int) error {
	i, f, err := get(c, number)
	if err != nil {
		return err
	}
	if err := openStage(number, i, f); err != nil {
		return err
	}
	return cancel(c, number, f.Stage)
}

func cancel(c *github.Client, number int, stage string) error {
	if stage != Cancelled {
		if _, err := move(c, number, stage, Cancelled, nil); err != nil {
			return err
		}
	}
	_, err := c.EditIssue(number, github.IssueEdit{State: "closed", StateReason: "not_planned"})
	return err
}

// Split creates one ticket per title at 2:drafting, with ticket number's
// priority and kind, and cancels ticket number. Each new ticket gets a log
// comment naming the ticket it was split from, and ticket number one naming
// them. When ticket number is a sub-issue, the new tickets become
// sub-issues of the same parent. A ticket created by a split is not split
// again.
func Split(c *github.Client, number int, titles []string) ([]Summary, error) {
	i, f, err := get(c, number)
	if err != nil {
		return nil, err
	}
	if err := openStage(number, i, f); err != nil {
		return nil, err
	}
	raw, err := c.Comments(number)
	if err != nil {
		return nil, err
	}
	for _, rc := range raw {
		cm := parseComment(rc)
		if from, ok := strings.CutPrefix(firstLine(cm.Text), "split from "); ok && cm.Kind == "log" && cm.Role == "dm" {
			return nil, fmt.Errorf("#%d was split from %s and cannot be split again", number, from)
		}
	}
	parent, hasParent, err := c.Parent(number)
	if err != nil {
		return nil, err
	}
	priority, kind := f.Priority, f.Kind
	if !slices.Contains(Priorities, priority) {
		priority = "medium"
	}
	if !slices.Contains(Kinds, kind) {
		kind = "feat"
	}
	var out []Summary
	var refs []string
	for _, title := range titles {
		child, cf, err := create(c, title, priority, kind, false)
		if err != nil {
			return out, err
		}
		out = append(out, Summary{Number: child.Number, Title: title, Fields: cf})
		refs = append(refs, fmt.Sprintf("#%d", child.Number))
		if _, err := c.CreateComment(child.Number, Marker("log", "dm")+fmt.Sprintf("split from #%d", number)); err != nil {
			return out, err
		}
		if hasParent {
			if err := c.AddSubIssue(parent.Number, child.ID); err != nil {
				return out, err
			}
		}
	}
	if _, err := c.CreateComment(number, Marker("log", "dm")+"split into "+strings.Join(refs, " ")); err != nil {
		return out, err
	}
	return out, cancel(c, number, f.Stage)
}

// Edit is a change to a ticket. Fields left empty stay as they are.
type Edit struct {
	Body     *string
	Priority string
	Kind     string
	Testing  string // "project" removes the ticket's own level
	Add      []int  // tickets to depend on
	Remove   []int  // tickets to depend on no more
}

// Amend applies e to ticket number, after it has checked every part of it,
// and returns the ticket's fields and the criteria it marked as revised at
// now.
func Amend(c *github.Client, number int, e Edit, now time.Time) (Fields, []string, error) {
	i, f, err := get(c, number)
	if err != nil {
		return f, nil, err
	}
	var add, remove []int64
	if len(e.Add) > 0 || len(e.Remove) > 0 {
		deps, err := c.BlockedBy(number)
		if err != nil {
			return f, nil, err
		}
		dep := func(m int) *github.Issue {
			for k := range deps {
				if deps[k].Number == m {
					return &deps[k]
				}
			}
			return nil
		}
		e.Add, e.Remove = unique(e.Add), unique(e.Remove)
		for _, m := range e.Add {
			if m == number {
				return f, nil, fmt.Errorf("#%d cannot depend on itself", number)
			}
			if dep(m) != nil {
				return f, nil, fmt.Errorf("#%d already depends on #%d", number, m)
			}
			d, _, err := get(c, m)
			if err != nil {
				return f, nil, err
			}
			add = append(add, d.ID)
		}
		for _, m := range e.Remove {
			d := dep(m)
			if d == nil {
				return f, nil, fmt.Errorf("#%d does not depend on #%d", number, m)
			}
			remove = append(remove, d.ID)
		}
	}

	// GitHub may still refuse a dependency, such as one that makes a cycle,
	// so the dependencies change first.
	for _, id := range add {
		if err := c.AddBlockedBy(number, id); err != nil {
			return f, nil, err
		}
	}
	for _, id := range remove {
		if err := c.RemoveBlockedBy(number, id); err != nil {
			return f, nil, err
		}
	}
	var revised []string
	if e.Body != nil {
		var body string
		body, revised = Revise(number, i.Body, *e.Body, now)
		if _, err := c.EditIssue(number, github.IssueEdit{Body: &body}); err != nil {
			return f, nil, err
		}
	}
	testing := e.Testing
	if testing == "project" {
		testing = ""
	}
	for _, l := range []struct {
		key, old, new string
		set           bool
	}{
		{"priority", f.Priority, e.Priority, e.Priority != ""},
		{"kind", f.Kind, e.Kind, e.Kind != ""},
		{"testing", f.Testing, testing, e.Testing != ""},
	} {
		if !l.set || l.old == l.new {
			continue
		}
		if l.new != "" {
			if _, err := c.AddLabels(number, l.key+":"+l.new); err != nil {
				return f, revised, err
			}
		}
		for v := range strings.SplitSeq(l.old, ",") {
			if v != "" && v != l.new {
				if _, err := c.TakeLabel(number, l.key+":"+v); err != nil {
					return f, revised, err
				}
			}
		}
	}
	_, f, err = get(c, number)
	return f, revised, err
}

// unique returns ns without repeats, in order.
func unique(ns []int) []int {
	var out []int
	for _, n := range ns {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

var heading = regexp.MustCompile(`^## +(.*?)\s*$`)

// Revise returns body, the new body of ticket number, with crew's marks on
// its acceptance criteria, and the criteria it marked as revised at now. A
// criterion whose text differs from the one in old is marked with now, one
// whose text is the same keeps the mark it had in old, and a new one has
// no mark.
func Revise(number int, old, body string, now time.Time) (string, []string) {
	line := regexp.MustCompile(fmt.Sprintf(`^(\s*[-*] \[(#%d-AC[0-9]+)\]\s+)(?:\(revised (\S+)\)\s*)?(.*)$`, number))
	type criterion struct{ text, mark string }
	before := map[string]criterion{}
	criteria(old, line, func(m []string) string {
		before[m[2]] = criterion{strings.TrimSpace(m[4]), m[3]}
		return ""
	})
	var revised []string
	body = criteria(body, line, func(m []string) string {
		mark := ""
		if b, ok := before[m[2]]; ok && b.text != strings.TrimSpace(m[4]) {
			mark = now.UTC().Format(markLayout)
			if !slices.Contains(revised, m[2]) {
				revised = append(revised, m[2])
			}
		} else if ok {
			mark = b.mark
		}
		if mark == "" {
			return m[1] + m[4]
		}
		return m[1] + "(revised " + mark + ") " + m[4]
	})
	return body, revised
}

// criteria calls f with the submatches of line for each line of body's
// acceptance criteria that line matches, and returns body with those lines
// replaced by what f returns.
func criteria(body string, line *regexp.Regexp, f func([]string) string) string {
	ls := strings.Split(body, "\n")
	in := false
	for k, l := range ls {
		if h := heading.FindStringSubmatch(l); h != nil {
			in = h[1] == "Acceptance criteria"
			continue
		}
		if m := line.FindStringSubmatch(l); in && m != nil {
			ls[k] = f(m)
		}
	}
	return strings.Join(ls, "\n")
}

// Ack marks the DM's decisions on ticket number as seen by the PM.
func Ack(c *github.Client, number int) error {
	return Post(c, number, "log", "pm", "ack")
}

// Block sets the blocked flag on ticket number and posts reason as a
// question by role, so that crew status shows it to the user.
func Block(c *github.Client, number int, role, reason string) error {
	if _, _, err := get(c, number); err != nil {
		return err
	}
	if _, err := c.AddLabels(number, labels.Blocked); err != nil {
		return err
	}
	_, err := c.CreateComment(number, Marker("question", role)+reason)
	return err
}

// Unblock clears the blocked flag on ticket number.
func Unblock(c *github.Client, number int) error {
	if _, _, err := get(c, number); err != nil {
		return err
	}
	took, err := c.TakeLabel(number, labels.Blocked)
	if err == nil && !took {
		err = fmt.Errorf("#%d is not blocked", number)
	}
	return err
}
