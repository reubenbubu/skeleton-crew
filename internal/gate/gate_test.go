package gate

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

// #8-AC4
func TestUntested(t *testing.T) {
	criteria := []string{"#1-AC1", "#1-AC2", "#1-AC3"}
	red := []string{"#1-AC1", "#1-AC3", "#2-AC1"}
	named := map[string][]string{"#1-AC1": {"tests/test_a.py"}, "#1-AC2": {"tests/test_b.py"}}
	got := Untested(criteria, red, named)
	want := []string{"tdd: no red log names #1-AC2", "tdd: no test names #1-AC3"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q; want %q", got, want)
	}
	if got := Untested(criteria[:1], red, named); got != nil {
		t.Fatalf("got %q", got)
	}
}

// #8-AC4
func TestBehaviour(t *testing.T) {
	if Behaviour([]string{"[feat:#1] document login", "[feat:#1] tidy docs"}) {
		t.Fatal("plain commits have behaviour")
	}
	for _, s := range []string{"[feat:#1] red: x", "[fix:#12] green: y", "[chore:#3] refactor: z"} {
		if !Behaviour([]string{"[feat:#1] docs", s}) {
			t.Fatalf("%q has no behaviour", s)
		}
	}
	if Behaviour([]string{"red: not crew's prefix"}) {
		t.Fatal("a subject without crew's prefix has behaviour")
	}
}

// #8-AC4 #8-AC5
func TestNamed(t *testing.T) {
	got := Named(map[string]string{
		"tests/test_b.py": "def test_x():  # #1-AC1 #1-AC2\n    pass\n# #1-AC1 again\n",
		"tests/test_a.py": "# #12-AC3\n",
		"tests/test_c.py": "nothing\n",
	})
	want := map[string][]string{"#1-AC1": {"tests/test_b.py"}, "#1-AC2": {"tests/test_b.py"}, "#12-AC3": {"tests/test_a.py"}}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for id, files := range want {
		if !slices.Equal(got[id], files) {
			t.Fatalf("got %q; want %q", got, want)
		}
	}
}

// #8-AC5
func TestSkipped(t *testing.T) {
	diff := `diff --git a/tests/test_a.py b/tests/test_a.py
index 1..2 100644
--- a/tests/test_a.py
+++ b/tests/test_a.py
@@ -3,0 +4,2 @@ def f():
+@pytest.mark.skip(reason="later")
+def test_x(): pass
@@ -9 +11 @@
-    pytest.skip("old")
+    assert True
diff --git a/tests/test_new.py b/tests/test_new.py
new file mode 100644
--- /dev/null
+++ b/tests/test_new.py
@@ -0,0 +1,3 @@
+import unittest
+
+@unittest.skip("no")
@@ -7,0 +11 @@
+++ x  # @unittest.skip
`
	got := Skipped(diff, []string{"@pytest.mark.skip", "pytest.skip(", "@unittest.skip"}, func(string) bool { return true })
	want := []string{
		"skipped: tests/test_a.py:4: @pytest.mark.skip",
		"skipped: tests/test_new.py:3: @unittest.skip",
		"skipped: tests/test_new.py:11: @unittest.skip",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q; want %q", got, want)
	}
	if got := Skipped(diff, nil, func(string) bool { return true }); got != nil {
		t.Fatalf("no markers: got %q", got)
	}
	if got := Skipped(diff, []string{"@unittest.skip"}, func(f string) bool { return f != "tests/test_new.py" }); got != nil {
		t.Fatalf("not a test file: got %q", got)
	}
}

// #8-AC5
func TestStale(t *testing.T) {
	early := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	named := map[string][]string{
		"#1-AC1": {"tests/test_a.py"},                    // fine
		"#1-AC2": {"tests/test_a.py"},                    // removed
		"#2-AC1": {"tests/test_b.py"},                    // cancelled
		"#3-AC1": {"tests/test_a.py", "tests/test_c.py"}, // revised after test_a, before test_c
		"#9-AC1": {"tests/test_c.py"},                    // no such ticket
	}
	tickets := map[int]ticket.Known{
		1: {Criteria: map[string]time.Time{"#1-AC1": {}}},
		2: {Criteria: map[string]time.Time{"#2-AC1": {}}, Cancelled: true},
		3: {Criteria: map[string]time.Time{"#3-AC1": time.Date(2026, 10, 16, 9, 12, 0, 0, time.UTC)}},
	}
	changed := map[string]time.Time{"tests/test_a.py": early, "tests/test_b.py": early, "tests/test_c.py": late}
	got, err := Stale(named, tickets, func(f string) (time.Time, error) { return changed[f], nil })
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"flag: tests/test_a.py names #1-AC2: removed",
		"flag: tests/test_a.py names #3-AC1: revised 2026-10-16T09:12Z",
		"flag: tests/test_b.py names #2-AC1: cancelled",
		"flag: tests/test_c.py names #9-AC1: removed",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q; want %q", got, want)
	}
}

// box stands in for the container engine's compose. answer, if set, gives
// the output and exit code of each call; otherwise every call succeeds.
type box struct {
	calls  []string
	answer func(args []string) (string, int)
}

func (b *box) run(dir string, args ...string) ([]byte, int, error) {
	b.calls = append(b.calls, dir+" "+strings.Join(args, " "))
	if b.answer == nil {
		return []byte("ok\n"), 0, nil
	}
	out, code := b.answer(args)
	return []byte(out), code, nil
}

// steps is a clock whose timers fire at once and move it forward.
type steps struct{ now time.Time }

func (s *steps) Now() time.Time { return s.now }
func (s *steps) After(d time.Duration) <-chan time.Time {
	s.now = s.now.Add(d)
	ch := make(chan time.Time, 1)
	ch <- s.now
	return ch
}

func newRun(b *box, commands map[string]string, log *bytes.Buffer) Run {
	return Run{
		Compose: b.run, Clock: &steps{now: clock.Epoch}, Poll: 2 * time.Second, Timeout: 10 * time.Second,
		Dir: "/gate", Project: "crew-1-gate", Commands: commands, Log: log,
	}
}

var all = map[string]string{"build": "b", "lint": "l", "test": "t", "test-changed": "tc", "run": "r", "health": "h", "e2e": "e"}

// #8-AC1 #8-AC6 #19-AC1 #19-AC3
func TestStepsRunInOrder(t *testing.T) {
	b := &box{}
	var log bytes.Buffer
	f, down, err := newRun(b, all, &log).Steps()
	if err != nil || f != nil || down != nil {
		t.Fatalf("got %+v %v", f, err)
	}
	want := []string{
		"/gate -p crew-1-gate down --volumes --remove-orphans",
		"/gate -p crew-1-gate build",
		`/gate -p crew-1-gate run --rm -T app sh -c b "$@" sh`,
		`/gate -p crew-1-gate run --rm -T app sh -c l "$@" sh`,
		`/gate -p crew-1-gate run --rm -T app sh -c t "$@" sh`,
		"/gate -p crew-1-gate up -d",
		"/gate -p crew-1-gate exec -T app sh -c h",
		"/gate -p crew-1-gate exec -T app sh -c e",
		"/gate -p crew-1-gate down --volumes --remove-orphans --rmi local",
	}
	if !slices.Equal(b.calls, want) {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(b.calls, "\n"), strings.Join(want, "\n"))
	}
	if got := log.String(); !strings.HasPrefix(got, "== fresh: down --volumes --remove-orphans\nok\n== fresh: exit 0\n") || !strings.Contains(got, "== e2e: exec -T app sh -c e\nok\n== e2e: exit 0\n") {
		t.Fatalf("log %q", got)
	}
}

// #8-AC1: steps the profile leaves out are skipped.
func TestStepsLeftOut(t *testing.T) {
	b := &box{}
	f, _, err := newRun(b, map[string]string{"test": "t", "test-changed": "tc"}, &bytes.Buffer{}).Steps()
	if err != nil || f != nil {
		t.Fatalf("got %+v %v", f, err)
	}
	want := []string{
		"/gate -p crew-1-gate down --volumes --remove-orphans",
		"/gate -p crew-1-gate build",
		`/gate -p crew-1-gate run --rm -T app sh -c t "$@" sh`,
		"/gate -p crew-1-gate down --volumes --remove-orphans --rmi local",
	}
	if !slices.Equal(b.calls, want) {
		t.Fatalf("calls %q", b.calls)
	}
}

// #8-AC2 #8-AC6: a failing step stops the gate, which still takes the
// stack down.
func TestStepFails(t *testing.T) {
	for _, c := range []struct {
		code int
		why  string
	}{{1, "lint: exit 1"}, {127, "lint: could not run: exit 127"}} {
		b := &box{answer: func(args []string) (string, int) {
			if slices.Contains(args, "l \"$@\"") {
				return "E501 line too long\n", c.code
			}
			return "", 0
		}}
		f, _, err := newRun(b, all, &bytes.Buffer{}).Steps()
		if err != nil || f == nil {
			t.Fatalf("got %+v %v", f, err)
		}
		if f.Step != "lint" || f.Reason != c.why || string(f.Output) != "E501 line too long\n" {
			t.Fatalf("failure %+v", f)
		}
		if len(b.calls) != 5 || b.calls[4] != "/gate -p crew-1-gate down --volumes --remove-orphans --rmi local" {
			t.Fatalf("calls %q", b.calls)
		}
	}
}

// #8-AC1: health is polled until it succeeds, up to the timeout.
func TestHealth(t *testing.T) {
	polls := 0
	b := &box{answer: func(args []string) (string, int) {
		if slices.Contains(args, "exec") && slices.Contains(args, "h") {
			polls++
			if polls < 3 {
				return "connection refused\n", 1
			}
		}
		return "", 0
	}}
	r := newRun(b, all, &bytes.Buffer{})
	f, _, err := r.Steps()
	if err != nil || f != nil || polls != 3 {
		t.Fatalf("got %+v %v after %d polls", f, err, polls)
	}
	if got := r.Clock.Now().Sub(clock.Epoch); got != 4*time.Second {
		t.Fatalf("waited %s", got)
	}

	polls = 0
	b = &box{answer: func(args []string) (string, int) {
		if slices.Contains(args, "exec") && slices.Contains(args, "h") {
			polls++
			return "connection refused\n", 1
		}
		return "", 0
	}}
	f, _, err = newRun(b, all, &bytes.Buffer{}).Steps()
	if err != nil || f == nil || f.Step != "health" || f.Reason != "health: not healthy within 10s" || string(f.Output) != "connection refused\n" {
		t.Fatalf("got %+v %v", f, err)
	}
	if polls != 6 || slices.ContainsFunc(b.calls, func(c string) bool { return strings.HasSuffix(c, " e") }) {
		t.Fatalf("%d polls, calls %q", polls, b.calls)
	}
	if last := b.calls[len(b.calls)-1]; last != "/gate -p crew-1-gate down --volumes --remove-orphans --rmi local" {
		t.Fatalf("last call %q", last)
	}
}

// #8-AC6: a stack that cannot be cleared is a failure, not a stale database.
func TestFreshFails(t *testing.T) {
	b := &box{answer: func(args []string) (string, int) {
		if slices.Contains(args, "down") {
			return "Error: volume in use\n", 1
		}
		return "", 0
	}}
	f, _, err := newRun(b, all, &bytes.Buffer{}).Steps()
	if err != nil || f == nil || f.Step != "fresh" || f.Reason != "fresh: exit 1" {
		t.Fatalf("got %+v %v", f, err)
	}
	if len(b.calls) != 2 {
		t.Fatalf("calls %q", b.calls)
	}
}

// #23-AC1 #23-AC3: a final down that fails is reported apart from the
// gate's own result, whether the gate passed or failed.
func TestTeardownFails(t *testing.T) {
	for _, lint := range []int{0, 1} {
		b := &box{answer: func(args []string) (string, int) {
			switch {
			case slices.Contains(args, "--rmi"):
				return "resource busy; down failed\n", 1
			case slices.Contains(args, "l \"$@\""):
				return "", lint
			}
			return "", 0
		}}
		f, down, err := newRun(b, all, &bytes.Buffer{}).Steps()
		if err != nil || (f != nil) != (lint != 0) {
			t.Fatalf("lint %d: got %+v %v", lint, f, err)
		}
		if down == nil || down.Step != "down" || down.Reason != "down: exit 1" || string(down.Output) != "resource busy; down failed\n" {
			t.Fatalf("lint %d: teardown %+v", lint, down)
		}
	}
}
