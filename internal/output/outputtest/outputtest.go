// Package outputtest checks command output against the output contract in
// tests.
package outputtest

import (
	"regexp"
	"testing"
)

var rules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"colour or escape sequence", regexp.MustCompile(`\x1b`)},
	{"URL", regexp.MustCompile(`(?i)\bhttps?://`)},
	{"relative time", regexp.MustCompile(`(?i)\b(\d+\s*[smhdw]|(\d+|an?)\s+(second|minute|hour|day|week|month|year)s?)\s+ago\b|\bin\s+(\d+|an?)\s+(second|minute|hour|day|week|month|year)s?\b|\b(yesterday|tomorrow|just now)\b`)},
	{"GitHub node ID", regexp.MustCompile(`\b[A-Z]{1,4}_kw[A-Za-z0-9_-]{4,}`)},
	{"API field", regexp.MustCompile(`\b(node_id|html_url|api_url|_links)\b`)},
	// Timestamps end in Z; an offset, even +00:00, or no zone at all is refused.
	{"timestamp not in UTC Z form", regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?(\.\d+)?([^Z0-9:.]|$)`)},
}

// Violations lists the output-contract rules that s breaks.
func Violations(s string) []string {
	var v []string
	for _, r := range rules {
		if r.re.MatchString(s) {
			v = append(v, r.name)
		}
	}
	return v
}

// CheckTerse fails the test if s breaks the output contract.
func CheckTerse(t testing.TB, s string) {
	t.Helper()
	if v := Violations(s); len(v) != 0 {
		t.Errorf("output breaks the output contract (%v):\n%s", v, s)
	}
}
