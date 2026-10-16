// Package profile loads the profile that describes a project's stack: its
// commands, the globs that identify its test files, the documents that seed
// .crew/docs/ and the scaffold for a new project.
package profile

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/toml"
	"github.com/reubenbubu/skeleton-crew/profiles"
)

type Profile struct {
	Name      string
	Commands  map[string]string // a step that does not apply has no command
	TestGlobs []string
	Docs      fs.FS // written under .crew/docs/
	Scaffold  fs.FS // written at the root of a new project
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
	cmds, globs, err := Parse(name+"/profile.toml", b)
	if err != nil {
		return Profile{}, err
	}
	docs, _ := fs.Sub(root, "docs")
	scaffold, _ := fs.Sub(root, "scaffold")
	return Profile{Name: name, Commands: cmds, TestGlobs: globs, Docs: docs, Scaffold: scaffold}, nil
}

// Parse reads the profile.toml called name, with contents b.
func Parse(name string, b []byte) (commands map[string]string, globs []string, err error) {
	doc, err := toml.Parse(name, b)
	if err != nil {
		return nil, nil, err
	}
	fail := func(line int, format string, a ...any) (map[string]string, []string, error) {
		return nil, nil, &toml.Error{File: name, Line: line, Msg: fmt.Sprintf(format, a...)}
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
	commands = map[string]string{}
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
		if k != "tests.globs" {
			return fail(v.Line, "unknown key %s", k)
		}
		if v.Kind != toml.Strings {
			return fail(v.Line, "%s must be an array of strings", k)
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
	return commands, globs, nil
}
