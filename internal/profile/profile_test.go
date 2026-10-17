package profile

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// #4-AC5 #4-AC6
func TestBuiltinPython(t *testing.T) {
	p, err := Builtin("python")
	if err != nil {
		t.Fatal(err)
	}
	wantCommands := map[string]string{
		"setup":        "pip install --no-cache-dir -e '.[dev]'",
		"build":        "python -m compileall -q src",
		"lint":         "ruff check . && ruff format --check .",
		"test":         "pytest -q",
		"test-changed": "pytest -q",
	}
	if p.Name != "python" || !maps.Equal(p.Commands, wantCommands) {
		t.Errorf("got %s %v", p.Name, p.Commands)
	}
	if !slices.Equal(p.TestGlobs, []string{"tests/**/test_*.py", "tests/**/*_test.py"}) {
		t.Errorf("globs: %v", p.TestGlobs)
	}
	// #8-AC5
	wantSkip := []string{"@pytest.mark.skip", "pytest.skip(", "@pytest.mark.xfail", "pytest.xfail(", "@unittest.skip", "unittest.SkipTest", "@unittest.expectedFailure"}
	if !slices.Equal(p.Skip, wantSkip) {
		t.Errorf("skip: %v", p.Skip)
	}
	docs := files(t, p.Docs)
	for _, want := range []string{"product.md", "architecture.md", "conventions.md", "adr/0001-python-stack.md"} {
		if !slices.Contains(docs, want) {
			t.Errorf("docs lack %s: %v", want, docs)
		}
	}
	scaffold := files(t, p.Scaffold)
	for _, want := range []string{"Containerfile", "compose.yaml", "pyproject.toml", "src/app/__init__.py", "tests/test_app.py"} {
		if !slices.Contains(scaffold, want) {
			t.Errorf("scaffold lacks %s: %v", want, scaffold)
		}
	}
}

func files(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	var names []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestUnknownProfile(t *testing.T) {
	for _, name := range []string{"cobol", "", "../python", "python/docs"} {
		if _, err := Builtin(name); err == nil || err.Error() != "unknown profile: "+name {
			t.Errorf("%q: got %v", name, err)
		}
	}
}

func TestParseManifest(t *testing.T) {
	ok := "[commands]\ntest = \"t\"\ntest-changed = \"c\"\n[tests]\nglobs = [\"a\"]\n"
	p, err := Parse("profile.toml", []byte(ok))
	if err != nil || !maps.Equal(p.Commands, map[string]string{"test": "t", "test-changed": "c"}) || !slices.Equal(p.TestGlobs, []string{"a"}) || p.Skip != nil {
		t.Fatalf("got %+v %v", p, err)
	}
	// #8-AC5
	p, err = Parse("profile.toml", []byte(ok+"skip = [\"@skip\"]\n"))
	if err != nil || !slices.Equal(p.Skip, []string{"@skip"}) {
		t.Fatalf("got %+v %v", p, err)
	}
	both := "[commands]\ntest = \"t\"\ntest-changed = \"c\"\n"
	for _, c := range []struct{ src, want string }{
		{ok + "[commands.x]\n", "f:6: unknown table commands.x"},
		{"[commands]\ndeploy = \"d\"\n", "f:2: unknown key commands.deploy"},
		{"[commands]\ntest = 1\n", "f:2: commands.test must be a string"},
		{"[commands]\ntest = \" \"\n", "f:2: commands.test is empty"},
		{both + "[tests]\nglobs = \"a\"\n", "f:5: tests.globs must be an array of strings"},
		{both + "[tests]\nglobs = []\n", "f:5: tests.globs is empty"},
		{both, "f: tests.globs is missing"},
		{both + "[tests]\nglobs = [\"a\"]\nskip = \"s\"\n", "f:6: tests.skip must be an array of strings"},
		{both + "[tests]\nglobs = [\"a\"]\nskip = [\" \"]\n", "f:6: tests.skip has an empty marker"},
		{"[commands]\ntest = \"t\"\n[tests]\nglobs = [\"a\"]\n", "f: commands.test-changed is missing"},
		{"[tests]\nglobs = [\"a\"]\n", "f: commands.test is missing"},
		{"x = 1\n", "f:1: unknown key x"},
		{"x = 1.5\n", "f:1: unsupported value"},
	} {
		if _, err := Parse("f", []byte(c.src)); err == nil || err.Error() != c.want {
			t.Errorf("%q:\ngot  %v\nwant %s", c.src, err, c.want)
		}
	}
}

func TestBuiltins(t *testing.T) {
	if got := Builtins(); !slices.Equal(got, []string{"java-spring", "node-ts", "python"}) {
		t.Fatalf("got %v", got)
	}
}

// Every image a built-in profile uses is named with its registry, because
// Podman resolves no short name without registries configured.
func TestBuiltinImagesAreFullyNamed(t *testing.T) {
	ref := regexp.MustCompile(`(?m)^(?:FROM|\s*image:)\s+(\S+)`)
	for _, name := range Builtins() {
		p, err := Builtin(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"Containerfile", "compose.yaml"} {
			b, err := fs.ReadFile(p.Scaffold, file)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
				if host, _, ok := strings.Cut(m[1], "/"); !ok || !strings.Contains(host, ".") {
					t.Errorf("%s %s: %s has no registry", name, file, m[1])
				}
			}
		}
	}
}

// #15-AC1: node-ts and java-spring provide every command, the test globs,
// a Containerfile, a compose.yaml, conventions and a scaffold.
func TestBuiltinNodeAndJava(t *testing.T) {
	for _, c := range []struct {
		name, adr string
		tests     []string // test files in the scaffold
		notTests  []string
	}{
		{"node-ts", "adr/0001-node-ts-stack.md", []string{"src/server.test.ts"}, []string{"src/server.ts", "e2e/health.e2e.ts"}},
		{"java-spring", "adr/0001-java-spring-stack.md", []string{"src/test/java/app/ApplicationTest.java"}, []string{"src/main/java/app/Application.java", "src/test/java/app/HealthE2E.java"}},
	} {
		p, err := Builtin(c.name)
		if err != nil {
			t.Fatal(err)
		}
		for _, cmd := range Commands {
			if p.Commands[cmd] == "" {
				t.Errorf("%s lacks %s", c.name, cmd)
			}
		}
		if len(p.TestGlobs) == 0 || len(p.Skip) == 0 {
			t.Errorf("%s: globs %v skip %v", c.name, p.TestGlobs, p.Skip)
		}
		docs := files(t, p.Docs)
		for _, want := range []string{"product.md", "architecture.md", "conventions.md", c.adr} {
			if !slices.Contains(docs, want) {
				t.Errorf("%s docs lack %s: %v", c.name, want, docs)
			}
		}
		scaffold := files(t, p.Scaffold)
		for _, want := range append([]string{"Containerfile", "compose.yaml", ".gitignore"}, append(c.tests, c.notTests...)...) {
			if !slices.Contains(scaffold, want) {
				t.Errorf("%s scaffold lacks %s: %v", c.name, want, scaffold)
			}
		}
		for _, f := range c.tests {
			if !p.IsTest(f) {
				t.Errorf("%s: %s is not a test", c.name, f)
			}
		}
		for _, f := range c.notTests {
			if p.IsTest(f) {
				t.Errorf("%s: %s is a test", c.name, f)
			}
		}
		compose, _ := fs.ReadFile(p.Scaffold, "compose.yaml")
		if !strings.Contains(string(compose), "\n  app:\n") {
			t.Errorf("%s compose.yaml has no app service", c.name)
		}
	}
}

// #15-AC3: a profile with a database states its migration rules: naming,
// forward-only changes and what a rebase needs.
func TestMigrationRules(t *testing.T) {
	withDB := false
	for _, name := range Builtins() {
		p, _ := Builtin(name)
		compose, _ := fs.ReadFile(p.Scaffold, "compose.yaml")
		conventions, _ := fs.ReadFile(p.Docs, "conventions.md")
		hasDB := strings.Contains(string(compose), "postgres")
		hasRules := strings.Contains(string(conventions), "## Database migrations")
		if hasDB != hasRules {
			t.Errorf("%s: database %v, migration rules %v", name, hasDB, hasRules)
		}
		if !hasRules {
			continue
		}
		withDB = true
		for _, want := range []string{"named `V<", "forward only", "never edited", "rebase", "fresh database"} {
			if !strings.Contains(string(conventions), want) {
				t.Errorf("%s migration rules lack %q", name, want)
			}
		}
	}
	if !withDB {
		t.Error("no built-in profile has a database")
	}
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const elixir = "[commands]\ntest = \"mix test\"\ntest-changed = \"mix test\"\n[tests]\nglobs = [\"test/**/*_test.exs\"]\n"

// #15-AC4: a custom profile in the project loads like a built-in one.
func TestLoadCustom(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".crew/profiles/elixir/profile.toml", elixir)
	writeFile(t, dir, ".crew/profiles/elixir/docs/conventions.md", "# Conventions\n")
	writeFile(t, dir, ".crew/profiles/elixir/scaffold/compose.yaml", "services:\n  app:\n")
	p, err := Load(dir, "elixir")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "elixir" || p.Custom != ".crew/profiles/elixir" || p.Commands["test"] != "mix test" || !p.IsTest("test/a/b_test.exs") {
		t.Fatalf("got %+v", p)
	}
	if got := files(t, p.Docs); !slices.Equal(got, []string{"conventions.md"}) {
		t.Errorf("docs %v", got)
	}
	if got := files(t, p.Scaffold); !slices.Equal(got, []string{"compose.yaml"}) {
		t.Errorf("scaffold %v", got)
	}
	// Without a custom profile of that name, the built-in one loads.
	if p, err := Load(dir, "python"); err != nil || p.Name != "python" || p.Custom != "" {
		t.Fatalf("python: %+v %v", p, err)
	}
}

func TestLoadCustomWithoutFolders(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".crew/profiles/elixir/profile.toml", elixir)
	p, err := Load(dir, "elixir")
	if err != nil || p.Docs != nil || p.Scaffold != nil {
		t.Fatalf("got %+v %v", p, err)
	}
}

func TestLoadRefusals(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".crew/profiles/python/profile.toml", elixir)
	writeFile(t, dir, ".crew/profiles/broken/profile.toml", "[commands]\ntest = \"t\"\n")
	for _, c := range []struct{ name, want string }{
		{"python", ".crew/profiles/python: python is a built-in profile; rename the custom one"},
		{"broken", ".crew/profiles/broken/profile.toml: commands.test-changed is missing"},
		{"cobol", "unknown profile: cobol"},
		{"../python", "unknown profile: ../python"},
	} {
		if _, err := Load(dir, c.name); err == nil || err.Error() != c.want {
			t.Errorf("%s: got %v, want %s", c.name, err, c.want)
		}
	}
	if _, err := Load(dir, "cobol"); !errors.Is(err, ErrUnknown) {
		t.Errorf("cobol: %v is not ErrUnknown", err)
	}
}
