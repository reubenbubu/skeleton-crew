package doctor

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/labels"
	"github.com/reubenbubu/skeleton-crew/internal/output/outputtest"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
)

func parse(t *testing.T, toml string) config.Config {
	t.Helper()
	c, err := config.Parse(config.Path, []byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const threeTiers = "profile = \"python\"\n[tiers.1]\neffort = \"low\"\n[tiers.2]\neffort = \"medium\"\n[tiers.3]\neffort = \"high\"\n"

// #16-AC2
func TestTiers(t *testing.T) {
	for _, tc := range []struct{ toml, want string }{
		{threeTiers, "ok tiers 1-3"},
		{"profile = \"python\"\n[tiers.1]\neffort = \"low\"\n", "ok tiers 1-1"},
		{"profile = \"python\"\n[tiers.1]\neffort = \"low\"\n[tiers.3]\neffort = \"high\"\n", "fail tiers: rank 2 is missing; ranks run from 1 upwards with no gaps"},
		{"profile = \"python\"\n[tiers.2]\neffort = \"low\"\n", "fail tiers: rank 1 is missing; ranks run from 1 upwards with no gaps"},
		{threeTiers + "[models]\nreviewer = 4\n", "fail tiers: models.reviewer is 4, but the highest rank is 3"},
		{threeTiers + "[models]\nmax_tier = 5\n", "fail tiers: models.max_tier is 5, but the highest rank is 3"},
	} {
		if got := Tiers(parse(t, tc.toml)).String(); got != tc.want {
			t.Errorf("got %q, want %q for\n%s", got, tc.want, tc.toml)
		}
	}
}

// #16-AC1
func TestLabels(t *testing.T) {
	c := parse(t, threeTiers)
	all := labels.All([]int{1, 2, 3})
	if got := Labels(all, c).String(); got != "ok labels 26" {
		t.Errorf("got %q", got)
	}
	// GitHub matches label names without regard to case.
	upper := append([]github.Label{}, all...)
	upper[0].Name = strings.ToUpper(upper[0].Name)
	if got := Labels(upper, c).String(); got != "ok labels 26" {
		t.Errorf("got %q", got)
	}
	var some []github.Label
	for _, l := range all {
		if l.Name != "blocked" && l.Name != "tier:3" {
			some = append(some, l)
		}
	}
	if got := Labels(some, c).String(); got != "fail labels: missing tier:3, blocked; run crew init" {
		t.Errorf("got %q", got)
	}
}

// #16-AC3
func TestKnowledgeBudget(t *testing.T) {
	files := []File{{".crew/docs/product.md", strings.Repeat("p", 12000)}, {".crew/docs/architecture.md", strings.Repeat("a", 10000)}, {".crew/docs/conventions.md", strings.Repeat("c", 10000)}}
	if got := Knowledge(files).String(); got != "ok knowledge 32000 bytes" {
		t.Errorf("at the budget: got %q", got)
	}
	files[2].Text += "c"
	want := "warn knowledge: 32001 bytes, over the budget of 32000; condense product.md, architecture.md and conventions.md, moving detail into ADRs"
	c := Knowledge(files)
	if got := c.String(); got != want {
		t.Errorf("over the budget: got %q", got)
	}
	if Failed([]Check{c}) {
		t.Error("a knowledge warning fails doctor")
	}
	outputtest.CheckTerse(t, want)
}

// #16-AC4
func TestConflicts(t *testing.T) {
	files := []File{
		{".crew/docs/conventions.md", "# Conventions\n\nRelease with `gh release create`.\nNever run `git push` yourself.\nSquash, then git  push the branch.\nUse git rebase instead of merging.\n"},
		{".crew/docs/adr/0002-flaky.md", "# Flaky tests\n\nMark them with @pytest.mark.skip until fixed.\nDo not use @pytest.mark.skip.\n"},
	}
	var got []string
	for _, c := range Conflicts(files, []string{"@pytest.mark.skip", "@unittest.skip"}) {
		got = append(got, c.String())
	}
	want := []string{
		"warn rules: .crew/docs/conventions.md:3: gh release; agents work through crew, never gh",
		"warn rules: .crew/docs/conventions.md:5: git push; crew makes every commit, push and branch",
		"warn rules: .crew/docs/conventions.md:6: git rebase; crew makes every commit, push and branch",
		"warn rules: .crew/docs/adr/0002-flaky.md:3: @pytest.mark.skip; tests are never skipped or disabled",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if c := Conflicts(files[:0], nil); len(c) != 1 || c[0].String() != "ok rules" {
		t.Errorf("no conflicts: got %v", c)
	}
}

// #16-AC4: the knowledge a built-in profile seeds contradicts no rule.
func TestBuiltinDocsHaveNoConflicts(t *testing.T) {
	for _, name := range profile.Builtins() {
		p, err := profile.Builtin(name)
		if err != nil {
			t.Fatal(err)
		}
		var files []File
		fs.WalkDir(p.Docs, ".", func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				b, _ := fs.ReadFile(p.Docs, path)
				files = append(files, File{path, string(b)})
			}
			return err
		})
		if len(files) == 0 {
			t.Fatalf("%s: no docs", name)
		}
		if c := Conflicts(files, p.Skip); len(c) != 1 || c[0].Status != OK {
			t.Errorf("%s: %v", name, c)
		}
		if c := Knowledge(files); c.Status != OK {
			t.Errorf("%s: %v", name, c)
		}
	}
}

// #16-AC5
func TestHarness(t *testing.T) {
	c := parse(t, threeTiers+"[harness.claude-code]\nwiring = \"wait\"\n")
	for _, tc := range []struct{ harness, want string }{
		{"claude-code", "ok harness claude-code wait"},
		{"opencode", "warn harness: opencode has no wiring in .crew/crew.toml; the lead DM runs the self-test at its next start"},
		{"", "warn harness: unknown; crew cannot tell which harness runs it"},
	} {
		if got := Harness(c, tc.harness).String(); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.harness, got, tc.want)
		}
	}
}
