//go:build containers

package profile_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/gate"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
)

// #15-AC2 #15-AC5: a project scaffolded from each built-in profile passes
// the gate's container steps, and does so again from a fresh database.
func TestScaffoldPassesGate(t *testing.T) {
	for _, name := range profile.Builtins() {
		t.Run(name, func(t *testing.T) {
			p, err := profile.Builtin(name)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.CopyFS(dir, p.Scaffold); err != nil {
				t.Fatal(err)
			}
			var log bytes.Buffer
			r := gate.Run{
				Compose: compose.Default(), Clock: clock.Real{}, Poll: 2 * time.Second, Timeout: 2 * time.Minute,
				Dir: dir, Project: fmt.Sprintf("crew-profiletest-%s-%d", name, os.Getpid()), Commands: p.Commands, Log: &log,
			}
			for k := 1; k <= 2; k++ {
				if f, err := r.Steps(); err != nil || f != nil {
					t.Fatalf("run %d: %+v %v\n%s", k, f, err, log.String())
				}
			}
			// The fast step for TDD runs on the scaffold's test files.
			var tests []string
			fs.WalkDir(p.Scaffold, ".", func(path string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && p.IsTest(path) {
					tests = append(tests, path)
				}
				return err
			})
			if len(tests) == 0 {
				t.Fatal("the scaffold has no test file")
			}
			project := fmt.Sprintf("crew-profiletest-%s-%d-changed", name, os.Getpid())
			defer compose.Default()(dir, "-p", project, "down", "--volumes", "--remove-orphans")
			if out, code, err := compose.ExecIn(compose.Default(), dir, project, p.Commands["test-changed"], tests); err != nil || code != 0 {
				t.Fatalf("test-changed %v: exit %d %v\n%s", tests, code, err, out)
			}
			// It fails on a failing test it is given.
			f := failing[name]
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(f.path)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, f.path), []byte(f.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if !p.IsTest(f.path) {
				t.Fatalf("%s is not a test file", f.path)
			}
			out, code, err := compose.ExecIn(compose.Default(), dir, project, p.Commands["test-changed"], []string{f.path})
			if err != nil || code == 0 || code >= 125 || !strings.Contains(string(out), "deliberately") {
				t.Fatalf("failing test-changed: exit %d %v\n%s", code, err, out)
			}
		})
	}
}

// failing is a test file in each profile's stack that fails with the word
// "deliberately".
var failing = map[string]struct{ path, body string }{
	"python":      {"tests/test_fails.py", "def test_fails():\n    assert False, \"deliberately\"\n"},
	"node-ts":     {"src/fails.test.ts", "import assert from \"node:assert/strict\";\nimport { test } from \"node:test\";\n\ntest(\"fails\", () => {\n  assert.fail(\"deliberately\");\n});\n"},
	"java-spring": {"src/test/java/app/FailsTest.java", "package app;\n\nimport static org.junit.jupiter.api.Assertions.fail;\n\nimport org.junit.jupiter.api.Test;\n\nclass FailsTest {\n  @Test\n  void fails() {\n    fail(\"deliberately\");\n  }\n}\n"},
}
