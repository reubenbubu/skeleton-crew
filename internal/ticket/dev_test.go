package ticket

import (
	"maps"
	"testing"
	"time"
)

// #8-AC4 #8-AC5
func TestCriteria(t *testing.T) {
	body := "## Goal\nx\n## Acceptance criteria\n" +
		"- [#12-AC1] (revised 2026-10-16T09:12Z) A known email gets a link.\n" +
		"- [#12-AC2] An unknown email gets an error.\n" +
		"* [#12-AC3]   (revised soon) Bad marks count as not revised.\n" +
		"- [#9-AC1] Another ticket's criterion is not this one's.\n" +
		"## Out of scope\n- [#12-AC4] Not a criterion here.\n"
	got := Criteria(12, body)
	want := map[string]time.Time{
		"#12-AC1": time.Date(2026, 10, 16, 9, 12, 0, 0, time.UTC),
		"#12-AC2": {},
		"#12-AC3": {},
	}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}
