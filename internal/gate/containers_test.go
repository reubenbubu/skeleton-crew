//go:build containers

package gate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/compose"
)

// stack is a compose project whose app keeps its data in a named volume,
// as a database would.
const stack = `services:
  app:
    image: docker.io/library/alpine:3.20
    command: ["sh", "-c", "trap exit TERM; sleep 600 & wait"]
    volumes:
      - data:/data
volumes:
  data:
`

func realRun(t *testing.T, commands map[string]string) (Run, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(stack), 0o644); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	return Run{
		Compose: compose.Default(), Clock: clock.Real{}, Poll: time.Second, Timeout: 30 * time.Second,
		Dir: dir, Project: fmt.Sprintf("crew-gatetest-%d", os.Getpid()), Commands: commands, Log: &log,
	}, &log
}

// #8-AC1 #8-AC6 #8-AC7: each run starts from an empty volume, so a test
// that fails when it finds the previous run's data passes every time.
func TestFreshDatabase(t *testing.T) {
	commands := map[string]string{
		"build":  "true",
		"lint":   "true",
		"test":   "test ! -e /data/marker && touch /data/marker",
		"health": "test -e /data/marker",
		"e2e":    "test -e /data/marker",
	}
	// The test step's container is gone before the stack starts, so health
	// finds the marker only because the volume outlives it within a run.
	r, log := realRun(t, commands)
	for k := 1; k <= 2; k++ {
		f, down, err := r.Steps()
		if err != nil || f != nil || down != nil {
			t.Fatalf("run %d: %+v %+v %v\n%s", k, f, down, err, log)
		}
	}
	commands["test"] = "echo broken; exit 3"
	f, _, err := r.Steps()
	if err != nil || f == nil || f.Reason != "test: exit 3" || !strings.HasPrefix(string(f.Output), "broken\n") {
		t.Fatalf("got %+v %v\n%s", f, err, log)
	}
}
