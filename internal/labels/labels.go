// Package labels defines the labels crew puts on tickets.
package labels

import (
	"fmt"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// Stage is one stage of the ticket lifecycle.
type Stage struct {
	Label   string // such as stage:4:in-progress
	Meaning string
}

// Stages lists the stages in lifecycle order.
var Stages = []Stage{
	{"stage:1:intake", "Created; waiting for the PM"},
	{"stage:2:drafting", "The PM is refining it"},
	{"stage:3:eligible", "Ready to be worked on"},
	{"stage:4:in-progress", "A Dev is implementing it"},
	{"stage:4:waiting-review", "The PR is ready and the gate has passed; waiting for the Reviewer"},
	{"stage:4:review-rejected", "The Reviewer rejected it; the DM starts a new attempt"},
	{"stage:5:review-approved", "Approved; the build-and-review loop is over"},
	{"stage:6:pending-merge", "Queued for merge into main"},
	{"stage:7:merged", "Merged; deployment starts if configured"},
	{"stage:8:deploying", "The deployment workflow is running"},
	{"stage:9:done", "Finished and closed"},
	{"stage:0:cancelled", "Will not be done; closed as not planned"},
}

// Blocked is the flag for a ticket that waits on something external or on
// the user.
const Blocked = "blocked"

// All returns every label crew uses, with tier labels for ranks.
func All(ranks []int) []github.Label {
	var ls []github.Label
	add := func(color, name, description string) {
		ls = append(ls, github.Label{Name: name, Color: color, Description: description})
	}
	for _, s := range Stages {
		add("1d76db", s.Label, s.Meaning)
	}
	add("fbca04", "priority:low", "Priority: low")
	add("fbca04", "priority:medium", "Priority: medium")
	add("fbca04", "priority:high", "Priority: high")
	add("fbca04", "priority:critical", "Skips the queue; set only when the user asks for it")
	add("5319e7", "kind:feat", "A new feature")
	add("5319e7", "kind:fix", "A bug fix")
	add("5319e7", "kind:chore", "Maintenance with no new behaviour")
	for _, r := range ranks {
		add("bfdadc", fmt.Sprintf("tier:%d", r), fmt.Sprintf("The current attempt uses rank %d", r))
	}
	add("c5def5", "testing:minimal", "Testing level for this ticket: minimal")
	add("c5def5", "testing:standard", "Testing level for this ticket: standard")
	add("c5def5", "testing:thorough", "Testing level for this ticket: thorough")
	add("b60205", Blocked, "Waiting on something external or on the user")
	return ls
}
