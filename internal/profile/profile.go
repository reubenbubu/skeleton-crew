// Package profile loads the profile that describes a project's stack: its
// commands, the globs that identify its test files, the documents that seed
// .crew/docs/ and the scaffold for a new project.
package profile

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/toml"
	"github.com/reubenbubu/skeleton-crew/profiles"
)

type Profile struct {
	Name      string
	Commands  map[string]string // a step that does not apply has no command
	TestGlobs []string
	Skip      []string // text that marks a skipped or disabled test
	Docs      fs.FS    // written under .crew/docs/
	Scaffold  fs.FS    // written at the root of a new project
}

// Commands lists the commands a profile may define.
var Commands = []string{"setup", "build", "lint", "test", "test-changed", "run", "health", "e2e"}

// required lists the commands every profile must define.
var required = []string{"test", "test-changed"}

// Builtin returns the built-in profile called name.
func Builtin(name string) (Profile, error) {
	unknown := fmt.Errorf("unknown profile: %s", name)
	if name == "" || strings.ContainsAny(name, `/\.`) {
		return Profile{}, unknown
	}
	root, err := fs.Sub(profiles.FS, name)
	if err != nil {
		return Profile{}, unknown
	}
	b, err := fs.ReadFile(root, "profile.toml")
	if err != nil {
		return Profile{}, unknown
	}
	p, err := Parse(name+"/profile.toml", b)
	if err != nil {
		return Profile{}, err
	}
	p.Name = name
	p.Docs, _ = fs.Sub(root, "docs")
	p.Scaffold, _ = fs.Sub(root, "scaffold")
	return p, nil
}

// Parse reads the profile.toml called name, with contents b, into a
// Profile without a name, docs or scaffold.
func Parse(name string, b []byte) (Profile, error) {
	doc, err := toml.Parse(name, b)
	if err != nil {
		return Profile{}, err
	}
	fail := func(line int, format string, a ...any) (Profile, error) {
		return Profile{}, &toml.Error{File: name, Line: line, Msg: fmt.Sprintf(format, a...)}
	}
	for _, t := range doc.Tables {
		if t != "commands" && t != "tests" {
			return fail(doc.TableLines[t], "unknown table %s", t)
		}
	}
	keys := make([]string, 0, len(doc.Values))
	for k := range doc.Values {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return doc.Values[a].Line - doc.Values[b].Line })
	commands := map[string]string{}
	var globs, skip []string
	globsSet := false
	for _, k := range keys {
		v := doc.Values[k]
		if c, ok := strings.CutPrefix(k, "commands."); ok && slices.Contains(Commands, c) {
			if v.Kind != toml.String {
				return fail(v.Line, "%s must be a string", k)
			}
			if strings.TrimSpace(v.Str) == "" {
				return fail(v.Line, "%s is empty", k)
			}
			commands[c] = v.Str
			continue
		}
		if k != "tests.globs" && k != "tests.skip" {
			return fail(v.Line, "unknown key %s", k)
		}
		if v.Kind != toml.Strings {
			return fail(v.Line, "%s must be an array of strings", k)
		}
		if k == "tests.skip" {
			if slices.ContainsFunc(v.Strs, func(m string) bool { return strings.TrimSpace(m) == "" }) {
				return fail(v.Line, "%s has an empty marker", k)
			}
			skip = v.Strs
			continue
		}
		if len(v.Strs) == 0 {
			return fail(v.Line, "%s is empty", k)
		}
		globs, globsSet = v.Strs, true
	}
	for _, c := range required {
		if commands[c] == "" {
			return fail(0, "commands.%s is missing", c)
		}
	}
	if !globsSet {
		return fail(0, "tests.globs is missing")
	}
	return Profile{Commands: commands, TestGlobs: globs, Skip: skip}, nil
}

// IsTest reports whether the file at name, a slash-separated path from the
// project root, is a test file: whether it matches one of the profile's
// globs. In a glob, ** matches any number of directories, including none.
func (p Profile) IsTest(name string) bool {
	for _, g := range p.TestGlobs {
		if match(strings.Split(g, "/"), strings.Split(name, "/")) {
			return true
		}
	}
	return false
}

func match(glob, name []string) bool {
	if len(glob) == 0 {
		return len(name) == 0
	}
	if glob[0] == "**" {
		for k := range len(name) + 1 {
			if match(glob[1:], name[k:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, _ := path.Match(glob[0], name[0])
	return ok && match(glob[1:], name[1:])
}
