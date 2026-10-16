package profile

import (
	"io/fs"
	"maps"
	"slices"
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
