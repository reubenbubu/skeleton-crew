// Package toml parses the strict subset of TOML that crew's files use:
// comments, [table] and [a.b] headers, bare keys, and values that are basic
// strings, decimal integers, booleans or one-line arrays of strings. Anything
// else is an error that names the file and line, so every file crew accepts
// is valid TOML.
package toml

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Kind int

const (
	String Kind = iota + 1
	Int
	Bool
	Strings
)

// Value is one parsed value and the line it is on.
type Value struct {
	Kind Kind
	Str  string
	Int  int64
	Bool bool
	Strs []string
	Line int
}

// Doc is a parsed file. Values are keyed by their full dotted name, such as
// tiers.1.model. Tables lists the headers in the order they appear, and
// TableLines gives the line of each.
type Doc struct {
	Values     map[string]Value
	Tables     []string
	TableLines map[string]int
}

// Error is a problem in a file, on one line unless Line is 0.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line == 0 {
		return e.File + ": " + e.Msg
	}
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
}

var (
	bareKey  = `[A-Za-z0-9_-]+`
	header   = regexp.MustCompile(`^\[(` + bareKey + `(?:\.` + bareKey + `)*)\]\s*(?:#.*)?$`)
	keyValue = regexp.MustCompile(`^(` + bareKey + `)\s*=\s*(.*)$`)
	integer  = regexp.MustCompile(`^[+-]?(?:0|[1-9][0-9]*)$`)
)

// Parse parses the file called name, with contents b.
func Parse(name string, b []byte) (Doc, error) {
	doc := Doc{Values: map[string]Value{}, TableLines: map[string]int{}}
	if !utf8.Valid(b) {
		return Doc{}, &Error{File: name, Msg: "not valid UTF-8"}
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	table := ""
	isTable := map[string]bool{}
	for i, raw := range strings.Split(string(b), "\n") {
		n := i + 1
		fail := func(msg string) (Doc, error) { return Doc{}, &Error{File: name, Line: n, Msg: msg} }
		line := strings.TrimSpace(raw)
		switch {
		case line == "" || line[0] == '#':
			continue
		case line[0] == '[':
			m := header.FindStringSubmatch(line)
			if m == nil {
				return fail("unsupported table")
			}
			table = m[1]
			if isTable[table] || doc.hasValueAtOrAbove(table) {
				return fail("duplicate table " + table)
			}
			isTable[table] = true
			doc.Tables = append(doc.Tables, table)
			doc.TableLines[table] = n
		default:
			m := keyValue.FindStringSubmatch(line)
			if m == nil {
				return fail("unsupported key")
			}
			key := m[1]
			if table != "" {
				key = table + "." + key
			}
			if _, dup := doc.Values[key]; dup || isTable[key] {
				return fail("duplicate key " + key)
			}
			v, ok := value(m[2])
			if !ok {
				return fail("unsupported value")
			}
			v.Line = n
			doc.Values[key] = v
		}
	}
	return doc, nil
}

// hasValueAtOrAbove reports whether table, or a table that contains it, is
// already a value.
func (d Doc) hasValueAtOrAbove(table string) bool {
	parts := strings.Split(table, ".")
	for i := range parts {
		if _, ok := d.Values[strings.Join(parts[:i+1], ".")]; ok {
			return true
		}
	}
	return false
}

// value parses s, which is everything after the = sign.
func value(s string) (Value, bool) {
	switch {
	case strings.HasPrefix(s, `"`):
		str, rest, ok := basicString(s)
		if !ok || !end(rest) {
			return Value{}, false
		}
		return Value{Kind: String, Str: str}, true
	case strings.HasPrefix(s, "["):
		var strs []string
		rest := strings.TrimSpace(s[1:])
		for !strings.HasPrefix(rest, "]") {
			str, r, ok := basicString(rest)
			if !ok {
				return Value{}, false
			}
			strs = append(strs, str)
			rest = strings.TrimSpace(r)
			if r, comma := strings.CutPrefix(rest, ","); comma {
				rest = strings.TrimSpace(r)
			} else if !strings.HasPrefix(rest, "]") {
				return Value{}, false
			}
		}
		if !end(rest[1:]) {
			return Value{}, false
		}
		return Value{Kind: Strings, Strs: strs}, true
	}
	tok, _, _ := strings.Cut(s, "#")
	tok = strings.TrimSpace(tok)
	switch {
	case tok == "true" || tok == "false":
		return Value{Kind: Bool, Bool: tok == "true"}, true
	case integer.MatchString(tok):
		i, err := strconv.ParseInt(tok, 10, 64)
		if err != nil {
			return Value{}, false
		}
		return Value{Kind: Int, Int: i}, true
	}
	return Value{}, false
}

// end reports whether s holds nothing but space and an optional comment.
func end(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s[0] == '#'
}

// basicString parses a "..." string at the start of s and returns it and the
// rest of s.
func basicString(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) || strings.HasPrefix(s, `"""`) {
		return "", "", false
	}
	var b strings.Builder
	for i := 1; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			return b.String(), s[i+1:], true
		case c == '\\' && i+1 < len(s):
			if r, ok := simpleEscapes[s[i+1]]; ok {
				b.WriteByte(r)
				i += 2
				continue
			}
			size := map[byte]int{'u': 4, 'U': 8}[s[i+1]]
			if size == 0 || i+2+size > len(s) {
				return "", "", false
			}
			code, err := strconv.ParseUint(s[i+2:i+2+size], 16, 32)
			if err != nil || !utf8.ValidRune(rune(code)) {
				return "", "", false
			}
			b.WriteRune(rune(code))
			i += 2 + size
		case c < 0x20 && c != '\t', c == 0x7f:
			return "", "", false
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", "", false
}

var simpleEscapes = map[byte]byte{'b': '\b', 't': '\t', 'n': '\n', 'f': '\f', 'r': '\r', '"': '"', '\\': '\\'}
