package compose

import (
	"regexp"
	"testing"
)

// #21-AC1 #21-AC3
func TestProjectNamesSeparateCheckouts(t *testing.T) {
	a, b := "/home/user/app", "/home/user/app-copy"
	for _, c := range []struct {
		name string
		of   func(root string) string
	}{
		{"dev", func(root string) string { return Project(root, 7) }},
		{"gate", func(root string) string { return GateProject(root, 7) }},
		{"doctor", DoctorProject},
	} {
		if c.of(a) == c.of(b) {
			t.Errorf("%s: two checkouts share %s", c.name, c.of(a))
		}
	}
	valid := regexp.MustCompile(`^crew-[0-9a-f]{8}-(7|7-gate|doctor)$`)
	for _, n := range []string{Project(a, 7), GateProject(a, 7), DoctorProject(a)} {
		if !valid.MatchString(n) {
			t.Errorf("name %q", n)
		}
	}
	if Project(a, 7) == Project(a, 8) || Project(a, 7) == GateProject(a, 7) {
		t.Error("projects of one checkout collide")
	}
}
