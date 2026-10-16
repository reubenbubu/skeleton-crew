package labels

import (
	"slices"
	"testing"
)

// #4-AC3
func TestAll(t *testing.T) {
	var names []string
	for _, l := range All([]int{1, 2, 3}) {
		if l.Color == "" || l.Description == "" || len(l.Description) > 100 {
			t.Errorf("%s: color %q description %q", l.Name, l.Color, l.Description)
		}
		names = append(names, l.Name)
	}
	want := []string{
		"stage:1:intake", "stage:2:drafting", "stage:3:eligible", "stage:4:in-progress",
		"stage:4:waiting-review", "stage:4:review-rejected", "stage:5:review-approved",
		"stage:6:pending-merge", "stage:7:merged", "stage:8:deploying", "stage:9:done", "stage:0:cancelled",
		"priority:low", "priority:medium", "priority:high", "priority:critical",
		"kind:feat", "kind:fix", "kind:chore",
		"tier:1", "tier:2", "tier:3",
		"testing:minimal", "testing:standard", "testing:thorough",
		"blocked",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("got  %v\nwant %v", names, want)
	}
}

// #4-AC3
func TestTierLabelsFollowTheRanks(t *testing.T) {
	var tiers []string
	for _, l := range All([]int{1, 2, 3, 4, 5}) {
		if len(l.Name) > 5 && l.Name[:5] == "tier:" {
			tiers = append(tiers, l.Name)
		}
	}
	if !slices.Equal(tiers, []string{"tier:1", "tier:2", "tier:3", "tier:4", "tier:5"}) {
		t.Fatalf("got %v", tiers)
	}
}
