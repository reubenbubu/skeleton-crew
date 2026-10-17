package ticket

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// Repairable returns ticket number, which must be at 6:pending-merge, with
// its fields.
func Repairable(c *github.Client, number int) (github.Issue, Fields, error) {
	i, f, err := get(c, number)
	if err == nil && f.Stage != "6:pending-merge" {
		err = stageChanged(number, f.Stage, "6:pending-merge")
	}
	return i, f, err
}

// Repair starts a repair attempt on ticket number, with fields f at
// 6:pending-merge: it posts reason and old, the commit the branch was at,
// as a log comment, then moves the ticket back to 4:in-progress at the
// same rank. The move is the one that goes back a stage; the approval is
// void, because the review must name a commit gated since then. A repair
// is not a failed attempt.
func Repair(c *github.Client, number int, f Fields, reason, old string) error {
	rank, err := strconv.Atoi(f.Tier)
	if err != nil {
		return fmt.Errorf("#%d has no single tier: tier=%s", number, f.Tier)
	}
	if err := Post(c, number, "log", "dm", "repair: "+reason+"\nbranch was at "+old); err != nil {
		return err
	}
	return attempt(c, number, "6:pending-merge", rank)
}

var repairedFrom = regexp.MustCompile(`(?m)^branch was at ([0-9a-f]{40})$`)

// RepairLease returns the commit the branch of ticket number was at when
// its newest repair started, or "" when it was never repaired.
func RepairLease(c *github.Client, number int) (string, error) {
	raw, err := c.Comments(number)
	if err != nil {
		return "", err
	}
	lease := ""
	for _, rc := range raw {
		cm := parseComment(rc)
		if cm.Kind == "log" && cm.Role == "dm" && strings.HasPrefix(cm.Text, "repair: ") {
			if m := repairedFrom.FindStringSubmatch(cm.Text); m != nil {
				lease = m[1]
			}
		}
	}
	return lease, nil
}
