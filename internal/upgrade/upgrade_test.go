package upgrade

import (
	"os"
	"strings"
	"testing"
)

const agents = "# Agent instructions\n\nRelease v0.1.0 notes stay.\n\n## Bootstrap\n\nSkeleton Crew pins `crew` version `v0.1.0`.\n\n" +
	"go install github.com/reubenbubu/skeleton-crew/cmd/crew@v0.1.0\nclone tag v0.1.0, not v0.1.01\n\n## Every session\n\nv0.1.0 is mentioned here too.\n"

// #17-AC4
func TestPinned(t *testing.T) {
	if v, err := Pinned([]byte(agents)); err != nil || v != "v0.1.0" {
		t.Fatalf("got %q %v", v, err)
	}
	for _, s := range []string{
		"# Agent instructions\n",
		"Skeleton Crew pins `crew` version `v0.1.0`.\n\n## Bootstrap\n\nNothing.\n",
		"## Bootstrap\n\n## Next\n\nSkeleton Crew pins `crew` version `v0.1.0`.\n",
		"## Bootstrap\n\nSkeleton Crew pins `crew` version `v01.1.0`.\n",
	} {
		if _, err := Pinned([]byte(s)); err != ErrNoPin {
			t.Errorf("%q: got %v", s, err)
		}
	}
}

// #17-AC4
func TestPinReplacesTheVersionInTheBootstrapOnly(t *testing.T) {
	got, err := Pin([]byte(agents), "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(agents, "`v0.1.0`.", "`v0.2.0`.", 1)
	want = strings.Replace(want, "crew@v0.1.0", "crew@v0.2.0", 1)
	want = strings.Replace(want, "tag v0.1.0,", "tag v0.2.0,", 1)
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := Pin([]byte(agents), "latest"); err == nil {
		t.Fatal("accepted an invalid version")
	}
}

// #17-AC3 #17-AC4
func TestTheSeedFilePinsARelease(t *testing.T) {
	b, err := os.ReadFile("../../project/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	v, err := Pinned(b)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := Pin(b, "v9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(moved), v) {
		t.Fatalf("a mention of %s is outside the bootstrap section or not a whole version", v)
	}
}

// #17-AC4
func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.10.0", "v0.9.0", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.1.1", false},
	} {
		if got, err := Newer(c.a, c.b); err != nil || got != c.want {
			t.Errorf("Newer(%s, %s) = %v %v", c.a, c.b, got, err)
		}
	}
	if _, err := Newer("v1.0.0-rc1", "v0.1.0"); err == nil {
		t.Error("accepted a prerelease tag")
	}
}
