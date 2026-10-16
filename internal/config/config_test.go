package config

import (
	"reflect"
	"strings"
	"testing"
)

func defaults(models ...string) Config {
	return Config{
		Profile: "python",
		Tiers: []Tier{
			{Rank: 1, Name: "light", Model: models[0], Effort: "medium"},
			{Rank: 2, Name: "standard", Model: models[1], Effort: "medium"},
			{Rank: 3, Name: "heavy", Model: models[2], Effort: "high"},
		},
		Reviewer:    2,
		MaxTier:     3,
		TopAttempts: 2,
		Testing:     "standard",
		Devs:        3,
	}
}

// #4-AC4
func TestTemplateHasTheReadmeDefaults(t *testing.T) {
	for _, c := range []struct {
		harness string
		want    Config
	}{
		{"claude-code", defaults("haiku", "sonnet", "opus")},
		{"opencode", defaults("", "", "")},
		{"", defaults("", "", "")},
	} {
		got, err := Parse(Path, Template("python", c.harness))
		if err != nil {
			t.Fatalf("%q: %v", c.harness, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\ngot  %+v\nwant %+v", c.harness, got, c.want)
		}
	}
}

// #4-AC4
func TestTemplateIsCommented(t *testing.T) {
	s := string(Template("python", "claude-code"))
	for _, want := range []string{"reviewer = 2", "max_tier = 3", "top_attempts = 2", `level = "standard"`, "devs = 3", "#"} {
		if !strings.Contains(s, want) {
			t.Errorf("template lacks %q:\n%s", want, s)
		}
	}
}

// #4-AC4
func TestOptionalKeysDefault(t *testing.T) {
	got, err := Parse("f", []byte("profile = \"python\"\n[tiers.1]\neffort = \"low\"\n[tiers.2]\neffort = \"max\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Profile:     "python",
		Tiers:       []Tier{{Rank: 1, Effort: "low"}, {Rank: 2, Effort: "max"}},
		Reviewer:    2,
		MaxTier:     2,
		TopAttempts: 2,
		Testing:     "standard",
		Devs:        3,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	got, err = Parse("f", []byte("profile = \"python\"\n[tiers.1]\neffort = \"low\"\n"))
	if err != nil || got.Reviewer != 1 {
		t.Errorf("one tier: reviewer=%d err=%v", got.Reviewer, err)
	}
}

// #4-AC4
func TestRejectsInvalidConfig(t *testing.T) {
	base := "profile = \"python\"\n[tiers.1]\neffort = \"low\"\n"
	for _, c := range []struct{ src, want string }{
		{base + "colour = \"red\"\n", "f:4: unknown key tiers.1.colour"},
		{base + "[testing]\nlevel = \"lots\"\n", "f:5: testing.level must be minimal, standard or thorough"},
		{base + "[testing]\nlevel = 3\n", "f:5: testing.level must be a string"},
		{base + "[parallel]\ndevs = \"3\"\n", "f:5: parallel.devs must be an integer"},
		{base + "[parallel]\ndevs = 0\n", "f:5: parallel.devs must be at least 1"},
		{base + "[models]\nreviewer = 0\n", "f:5: models.reviewer must be at least 1"},
		{base + "[escalation]\ntop_attempts = 0\n", "f:5: escalation.top_attempts must be at least 1"},
		{base + "[tiers.2]\neffort = \"huge\"\n", "f:5: tiers.2.effort must be low, medium, high or max"},
		{base + "[tiers.2]\nname = \"x\"\n", "f:4: tiers.2.effort is missing"},
		{base + "[tiers.x]\neffort = \"low\"\n", "f:4: unknown table tiers.x"},
		{base + "[tiers.0]\neffort = \"low\"\n", "f:4: unknown table tiers.0"},
		{base + "[tiers.01]\neffort = \"low\"\n", "f:4: unknown table tiers.01"},
		{base + "[tiers.99999999999999999999]\neffort = \"low\"\n", "f:4: unknown table tiers.99999999999999999999"},
		{base + "[bogus]\n", "f:4: unknown table bogus"},
		{base + "x = 1\n", "f:4: unknown key tiers.1.x"},
		{"[tiers.1]\neffort = \"low\"\n", "f: profile is missing"},
		{"profile = \"python\"\n", "f: no tiers"},
		{"profile = \"python\"\n[tiers]\n", "f: no tiers"},
		{"profile = 1.5\n", "f:1: unsupported value"},
	} {
		_, err := Parse("f", []byte(c.src))
		if err == nil || err.Error() != c.want {
			t.Errorf("%q:\ngot  %v\nwant %s", c.src, err, c.want)
		}
	}
}

// #4-AC4
func TestDetectHarness(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"AI_AGENT": "claude-code_2-1-296_agent"}, "claude-code"},
		{map[string]string{"AI_AGENT": "opencode"}, "opencode"},
		{map[string]string{"AI_AGENT": "some-new-harness_1"}, "some-new-harness"},
		{map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{map[string]string{"OPENCODE": "1"}, "opencode"},
		{map[string]string{"AI_AGENT": "opencode", "CLAUDECODE": "1"}, "opencode"},
		{map[string]string{"CLAUDECODE": "0"}, ""},
		{map[string]string{}, ""},
	} {
		got := DetectHarness(func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("%v: got %q, want %q", c.env, got, c.want)
		}
	}
}

// #13-AC5
func TestHarnessRecords(t *testing.T) {
	src := string(Template("python", "claude-code")) +
		"\n[harness.claude-code]\nwiring = \"wait\"\n" +
		"\n[harness.opencode]\nwiring = \"own\"\nstart = \"Load the \\\"crew-wake\\\" plugin.\"\nfiles = [\".opencode/plugins/crew-wake.ts\"]\n"
	got, err := Parse(Path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Harness{
		"claude-code": {Wiring: "wait"},
		"opencode":    {Wiring: "own", Start: `Load the "crew-wake" plugin.`, Files: []string{".opencode/plugins/crew-wake.ts"}},
	}
	if !reflect.DeepEqual(got.Harnesses, want) {
		t.Errorf("got  %+v\nwant %+v", got.Harnesses, want)
	}

	base := "profile = \"python\"\n[tiers.1]\neffort = \"low\"\n"
	for _, c := range []struct{ src, want string }{
		{base + "[harness.x]\nwiring = \"poll\"\n", "f:5: harness.x.wiring must be wait or own"},
		{base + "[harness.x]\nstart = \"go\"\n", "f:4: harness.x.wiring is missing"},
		{base + "[harness.x]\nwiring = \"own\"\n", "f:4: harness.x.start is missing"},
		{base + "[harness.x]\nwiring = \"wait\"\nfiles = \"a\"\n", "f:6: harness.x.files must be an array of strings"},
		{base + "[harness.x]\nwiring = \"wait\"\nbogus = 1\n", "f:6: unknown key harness.x.bogus"},
		{base + "[harness.x.y]\n", "f:4: unknown table harness.x.y"},
	} {
		_, err := Parse("f", []byte(c.src))
		if err == nil || err.Error() != c.want {
			t.Errorf("%q:\ngot  %v\nwant %s", c.src, err, c.want)
		}
	}
}

// #13-AC5
func TestRecordReplacesOnlyTheHarnessTable(t *testing.T) {
	src := "profile = \"python\"\n[tiers.1]\neffort = \"low\"\n\n[harness.opencode]\nwiring = \"own\"\nstart = \"Load it.\"\nfiles = [\"a.ts\"]\n\n[harness.claude-code]\nwiring = \"wait\"\n"
	got := string(Record([]byte(src), "opencode", Harness{Wiring: "wait"}))
	want := "profile = \"python\"\n[tiers.1]\neffort = \"low\"\n\n[harness.claude-code]\nwiring = \"wait\"\n\n[harness.opencode]\nwiring = \"wait\"\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Comments above the next table stay with it.
	src = "profile = \"python\"\n\n[harness.x]\nwiring = \"wait\"\n\n# escalation\n[escalation]\ntop_attempts = 2\n"
	want = "profile = \"python\"\n\n# escalation\n[escalation]\ntop_attempts = 2\n\n[harness.x]\nwiring = \"wait\"\n"
	if got := string(Record([]byte(src), "x", Harness{Wiring: "wait"})); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	got = string(Record([]byte("profile = \"python\""), "x", Harness{Wiring: "own", Start: "Say \"go\".", Files: []string{"a", "b"}}))
	want = "profile = \"python\"\n\n[harness.x]\nwiring = \"own\"\nstart = \"Say \\\"go\\\".\"\nfiles = [\"a\", \"b\"]\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := Parse("f", []byte(got)); err == nil || !strings.Contains(err.Error(), "no tiers") {
		t.Errorf("record does not parse: %v", err)
	}
}
