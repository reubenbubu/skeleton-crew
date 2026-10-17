package ticket

import (
	"slices"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// A repair reaches 4:in-progress from 6:pending-merge and is no attempt.
func TestRepairIsNoAttempt(t *testing.T) {
	var events []github.Event
	label := func(event, name string) {
		events = append(events, github.Event{Event: event, Label: &github.Label{Name: name}})
	}
	label("labeled", "tier:1")
	label("labeled", "stage:4:in-progress")
	label("unlabeled", "stage:3:eligible")
	label("labeled", "stage:4:review-rejected")
	label("labeled", "tier:2")
	label("labeled", "stage:4:in-progress")
	label("labeled", "stage:5:review-approved")
	label("labeled", "stage:6:pending-merge")
	label("labeled", "stage:4:in-progress")
	label("unlabeled", "stage:6:pending-merge")
	if tiers, _ := attempts(events); !slices.Equal(tiers, []string{"1", "2"}) {
		t.Fatalf("tiers %q", tiers)
	}
}
