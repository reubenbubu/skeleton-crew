// Package output holds the formatting rules shared by every command: terse
// lines for agents, one-line JSON for --json, and absolute UTC timestamps.
package output

import (
	"encoding/json"
	"io"
	"time"
)

// Time formats t as an absolute UTC timestamp to the minute, such as
// 2026-10-15T09:12Z.
func Time(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04Z")
}

// JSON writes v as one line of JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
