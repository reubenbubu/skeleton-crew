package toml

import (
	"slices"
	"testing"
)

// #4-AC4
func TestParsesTheSubset(t *testing.T) {
	src := `# comment
profile = "python"   # trailing comment

[tiers.1]
name   = "light"
model  = ""
effort = "say \"hi\"\t\\ é"

[models]
reviewer = 2
max_tier = -3
on = true
off = false

[tests]
globs = ["a/**/*.py", "b # not a comment", ]
none = []
`
	doc, err := Parse("crew.toml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Value{
		"profile":         {Kind: String, Str: "python", Line: 2},
		"tiers.1.name":    {Kind: String, Str: "light", Line: 5},
		"tiers.1.model":   {Kind: String, Str: "", Line: 6},
		"tiers.1.effort":  {Kind: String, Str: "say \"hi\"\t\\ é", Line: 7},
		"models.reviewer": {Kind: Int, Int: 2, Line: 10},
		"models.max_tier": {Kind: Int, Int: -3, Line: 11},
		"models.on":       {Kind: Bool, Bool: true, Line: 12},
		"models.off":      {Kind: Bool, Bool: false, Line: 13},
		"tests.globs":     {Kind: Strings, Strs: []string{"a/**/*.py", "b # not a comment"}, Line: 16},
		"tests.none":      {Kind: Strings, Strs: nil, Line: 17},
	}
	if len(doc.Values) != len(want) {
		t.Errorf("got %d values, want %d: %v", len(doc.Values), len(want), doc.Values)
	}
	for k, w := range want {
		g, ok := doc.Values[k]
		if !ok {
			t.Errorf("missing %s", k)
			continue
		}
		if g.Kind != w.Kind || g.Str != w.Str || g.Int != w.Int || g.Bool != w.Bool || !slices.Equal(g.Strs, w.Strs) || g.Line != w.Line {
			t.Errorf("%s: got %+v, want %+v", k, g, w)
		}
	}
	if !slices.Equal(doc.Tables, []string{"tiers.1", "models", "tests"}) {
		t.Errorf("tables: %v", doc.Tables)
	}
}

// #4-AC4
func TestRejectsAnythingOutsideTheSubset(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"a = 1.5", "f:1: unsupported value"},
		{"a = 1979-05-27", "f:1: unsupported value"},
		{"a = 'literal'", "f:1: unsupported value"},
		{`a = """multi"""`, "f:1: unsupported value"},
		{"a = { b = 1 }", "f:1: unsupported value"},
		{"a = [1, 2]", "f:1: unsupported value"},
		{`a = ["x"`, "f:1: unsupported value"},
		{"a = 0x10", "f:1: unsupported value"},
		{"a = 1_000", "f:1: unsupported value"},
		{"a = 01", "f:1: unsupported value"},
		{"a = True", "f:1: unsupported value"},
		{"a =", "f:1: unsupported value"},
		{`a = "x" y`, "f:1: unsupported value"},
		{`a = "\q"`, "f:1: unsupported value"},
		{`a = "open`, "f:1: unsupported value"},
		{"a = 99999999999999999999", "f:1: unsupported value"},
		{`"a" = 1`, "f:1: unsupported key"},
		{"a.b = 1", "f:1: unsupported key"},
		{"just words", "f:1: unsupported key"},
		{"[[array]]", "f:1: unsupported table"},
		{"[a b]", "f:1: unsupported table"},
		{"[]", "f:1: unsupported table"},
		{"[a] x", "f:1: unsupported table"},
		{"\na = 1\na = 2", "f:3: duplicate key a"},
		{"[t]\n[t]", "f:2: duplicate table t"},
		{"[t]\na = 1\n[u]\n[t]", "f:4: duplicate table t"},
	} {
		_, err := Parse("f", []byte(c.src))
		if err == nil || err.Error() != c.want {
			t.Errorf("%q: got %v, want %s", c.src, err, c.want)
		}
	}
}

// #4-AC4
func TestKeyCollidingWithTable(t *testing.T) {
	_, err := Parse("f", []byte("[a]\nb = 1\n[a.b]"))
	if err == nil || err.Error() != "f:3: duplicate table a.b" {
		t.Fatalf("got %v", err)
	}
}

// #4-AC4: files saved with a byte order mark are still TOML.
func TestByteOrderMark(t *testing.T) {
	doc, err := Parse("f", []byte("\xef\xbb\xbfa = 1\n"))
	if err != nil || doc.Values["a"].Int != 1 {
		t.Fatalf("got %v %v", doc.Values, err)
	}
}

// #4-AC4
func TestRejectsInvalidUTF8(t *testing.T) {
	if _, err := Parse("f", []byte("a = \"\xff\"\n")); err == nil || err.Error() != "f: not valid UTF-8" {
		t.Fatalf("got %v", err)
	}
}
