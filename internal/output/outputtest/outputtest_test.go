package outputtest

import "testing"

// #2-AC4
func TestViolations(t *testing.T) {
	clean := "#12 Login with email\nstage=4:in-progress pr=#31\n2026-10-15T09:12Z eligible\n"
	if v := Violations(clean); len(v) != 0 {
		t.Fatalf("clean output flagged: %v", v)
	}
	for _, bad := range []string{
		"\x1b[31mfailed\x1b[0m",
		"see https://github.com/o/r/pull/31",
		"updated 2h ago",
		"updated 3 minutes ago",
		"updated an hour ago",
		"retry in 5 minutes",
		"at 2026-10-15T09:12:00 local",
		"at 2026-10-15T09:12",
		"id=I_kwDOAbc123",
		"at 2026-10-15T11:12+02:00",
		`{"node_id":"x"}`,
	} {
		if v := Violations(bad); len(v) == 0 {
			t.Errorf("not flagged: %q", bad)
		}
	}
}
