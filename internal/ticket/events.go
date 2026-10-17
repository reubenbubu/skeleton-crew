package ticket

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
)

// Event is something on ticket Number that the lead DM wakes for.
type Event struct {
	At     time.Time `json:"at"`
	Number int       `json:"number"`
	What   string    `json:"what"`
}

// wakes are the stages whose arrival wakes the lead DM: a ticket became
// eligible, or a Dev or Reviewer finished.
var wakes = []string{"3:eligible", "4:waiting-review", "4:review-rejected", "5:review-approved"}

// Events lists what happened on open tickets from since to now, both
// included, that the lead DM acts on: a ticket reaching a stage in wakes,
// blocked cleared, a Dev question, an answer that is not the DM's, a human
// comment, a ticket at a DM stage going stale, and the newest deployment
// run of a ticket at 7:merged or 8:deploying finishing. The lead DM's own
// work is left out. They are sorted by time, then by ticket.
func Events(c *github.Client, since, now time.Time) ([]Event, error) {
	issues, err := c.Issues("open")
	if err != nil {
		return nil, err
	}
	in := func(t time.Time) bool { return !t.Before(since) && !t.After(now) }
	var out []Event
	add := func(at time.Time, n int, what string) { out = append(out, Event{at.UTC(), n, what}) }
	touched := map[int]bool{}
	anyTouched := false
	for _, i := range issues {
		f := fieldsOf(i)
		if f.Stage == "" {
			continue
		}
		// A run finishing changes nothing on the issue, so it is looked up
		// whether or not the issue changed.
		if f.Stage == "7:merged" || f.Stage == "8:deploying" {
			_, run, err := deployment(c, i, f)
			if err != nil {
				return nil, err
			}
			if run != nil && run.Status == "completed" && in(run.UpdatedAt) {
				add(run.UpdatedAt, i.Number, "deployment finished: "+run.Conclusion)
			}
		}
		// An issue's updated_at is never earlier than its last stage
		// change, so one that cannot have gone stale since needs no events.
		d, dwells := dwell[f.Stage]
		stale := dwells && !pmStage(f.Stage) && !f.Blocked &&
			!i.UpdatedAt.Add(d.after).Before(since) && !now.Before(i.CreatedAt.Add(d.after))
		touched[i.Number] = !i.UpdatedAt.Before(since)
		anyTouched = anyTouched || touched[i.Number]
		if !touched[i.Number] && !stale {
			continue
		}
		events, err := c.Events(i.Number)
		if err != nil {
			return nil, err
		}
		entered := i.CreatedAt
		for _, e := range events {
			if e.Label == nil {
				continue
			}
			stage, isStage := strings.CutPrefix(e.Label.Name, "stage:")
			if e.Event == "labeled" && isStage && stage == f.Stage {
				entered = e.CreatedAt
			}
			switch {
			case !in(e.CreatedAt):
			case e.Event == "labeled" && isStage && slices.Contains(wakes, stage):
				add(e.CreatedAt, i.Number, "reached "+stage)
			case e.Event == "unlabeled" && e.Label.Name == labels.Blocked:
				add(e.CreatedAt, i.Number, "unblocked")
			}
		}
		if at := entered.Add(d.after); stale && in(at) {
			add(at, i.Number, "stale at "+f.Stage+": "+d.flag)
		}
	}

	if anyTouched {
		raw, err := c.RepoComments(since)
		if err != nil {
			return nil, err
		}
		for _, rc := range raw {
			n := rc.Issue()
			if !touched[n] || !in(rc.CreatedAt) {
				continue
			}
			switch cm := parseComment(rc); {
			case cm.Kind == "question" && cm.Role == "dev":
				add(cm.At, n, "dev question: "+firstLine(cm.Text))
			case cm.Kind == "answer" && cm.Role != "dm":
				add(cm.At, n, "answer by "+cm.Role+": "+firstLine(cm.Text))
			case cm.Kind == Human:
				add(cm.At, n, "comment by "+cm.Author+": "+firstLine(cm.Text))
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Event) int {
		return cmp.Or(a.At.Compare(b.At), a.Number-b.Number)
	})
	return out, nil
}
