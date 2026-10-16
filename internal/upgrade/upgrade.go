// Package upgrade reads and moves the crew version that a project's
// AGENTS.md pins in its bootstrap section.
package upgrade

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Source is the repository crew is released from.
const Source = "reubenbubu/skeleton-crew"

// File is the file that pins the version, relative to the project root.
const File = "AGENTS.md"

var (
	version = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	pin     = regexp.MustCompile("pins `crew` version `(v[0-9]+\\.[0-9]+\\.[0-9]+)`")
)

// ErrNoPin means AGENTS.md has no bootstrap section that pins a version.
var ErrNoPin = errors.New(File + ": no crew pin; want a ## Bootstrap section that pins `crew` version `vX.Y.Z`")

// section returns the start and end of the bootstrap section of agents:
// from its "## Bootstrap" heading to the next "## " heading.
func section(agents string) (start, end int, ok bool) {
	at := 0
	for _, line := range strings.SplitAfter(agents, "\n") {
		t := strings.TrimRight(line, "\r\n")
		switch {
		case ok && strings.HasPrefix(t, "## "):
			return start, at, true
		case t == "## Bootstrap":
			start, ok = at, true
		}
		at += len(line)
	}
	return start, len(agents), ok
}

// Pinned returns the version that agents, the contents of AGENTS.md, pins.
func Pinned(agents []byte) (string, error) {
	s := string(agents)
	start, end, ok := section(s)
	if !ok {
		return "", ErrNoPin
	}
	m := pin.FindStringSubmatch(s[start:end])
	if m == nil || !version.MatchString(m[1]) {
		return "", ErrNoPin
	}
	return m[1], nil
}

// Pin returns agents with every mention of its pinned version in the
// bootstrap section replaced by v.
func Pin(agents []byte, v string) ([]byte, error) {
	old, err := Pinned(agents)
	if err != nil {
		return nil, err
	}
	if !version.MatchString(v) {
		return nil, errors.New("invalid version: " + v)
	}
	s := string(agents)
	start, end, _ := section(s)
	mention := regexp.MustCompile(`\b` + regexp.QuoteMeta(old) + `\b`)
	return []byte(s[:start] + mention.ReplaceAllLiteralString(s[start:end], v) + s[end:]), nil
}

// Newer reports whether version a is newer than version b. Both have the
// form vMAJOR.MINOR.PATCH.
func Newer(a, b string) (bool, error) {
	pa, pb := version.FindStringSubmatch(a), version.FindStringSubmatch(b)
	switch {
	case pa == nil:
		return false, errors.New("invalid version: " + a)
	case pb == nil:
		return false, errors.New("invalid version: " + b)
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y, nil
		}
	}
	return false, nil
}
