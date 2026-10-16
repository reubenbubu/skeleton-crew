// Package brief holds the role rules, embedded into the binary, and builds
// the parts of an agent's briefing.
package brief

import (
	"embed"
	"path"
	"regexp"
	"slices"
	"strings"
)

//go:embed rules/*.md
var files embed.FS

// Roles are the roles that have a briefing.
var Roles = []string{"pm", "dm", "dev", "reviewer"}

// Common is the part of the rules that every role shares.
var Common = read("common")

func read(name string) string {
	b, err := files.ReadFile("rules/" + name + ".md")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Rules returns the rules of role: Common, then the role's own.
func Rules(role string) (string, bool) {
	if !slices.Contains(Roles, role) {
		return "", false
	}
	return Common + "\n" + read(role), true
}

// ADRDir is where the ADRs live, relative to the project root.
const ADRDir = ".crew/docs/adr"

// ADR is one line of the ADR index.
type ADR struct {
	Number  string `json:"number"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Path    string `json:"path"`
}

var (
	adrName  = regexp.MustCompile(`^([0-9]+)-(.+)\.md$`)
	adrTitle = regexp.MustCompile(`^#\s+(.*?)\s*$`)
)

// ParseADR reads the ADR at p, with contents text. It reports false for a
// file that is not an ADR: one outside ADRDir, or not named
// <number>-<slug>.md. The title is the first heading, without the
// ADR's number, or else the slug. The summary is the text after "Summary:".
func ParseADR(p, text string) (ADR, bool) {
	dir, name := path.Split(p)
	m := adrName.FindStringSubmatch(name)
	if dir != ADRDir+"/" || m == nil {
		return ADR{}, false
	}
	a := ADR{Number: m[1], Title: m[2], Path: p}
	title := false
	for _, l := range strings.Split(text, "\n") {
		if t := adrTitle.FindStringSubmatch(l); t != nil && !title {
			title = true
			if t := strings.TrimSpace(strings.TrimPrefix(t[1], a.Number+" ")); t != "" {
				a.Title = t
			}
		} else if s, ok := strings.CutPrefix(l, "Summary:"); ok && a.Summary == "" {
			a.Summary = strings.TrimSpace(s)
		}
	}
	return a, true
}

// Line formats a as one line of the index, such as
// "0001 Python stack: Python 3.13 in containers.".
func (a ADR) Line() string {
	s := a.Number + " " + a.Title
	if a.Summary != "" {
		s += ": " + a.Summary
	}
	return s
}
