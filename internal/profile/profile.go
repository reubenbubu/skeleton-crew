// Package profile loads the profile that describes a project's stack: its
// commands, the globs that identify its test files, the documents that seed
// .crew/docs/ and the scaffold for a new project.
package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
	Docs      fs.FS    // written under .crew/docs/; nil when there are none
	Scaffold  fs.FS    // written at the root of a new project; nil when there is none
	Custom    string   // the custom profile's directory in the project; "" when built in
}

// Commands lists the commands a profile may define.
var Commands = []string{"setup", "build", "lint", "test", "test-changed", "run", "health", "e2e"}

// required lists the commands every profile must define.
var required = []string{"test", "test-changed"}

// Dir is the directory, relative to the project root, that holds the
// project's custom profiles, one directory each.
const Dir = ".crew/profiles"

// Builtins lists the names of the built-in profiles.
func Builtins() []string {
	entries, _ := fs.ReadDir(profiles.FS, ".")
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// Builtin returns the built-in profile called name.
func Builtin(name string) (Profile, error) {
	if !validName(name) {
		return Profile{}, unknown(name)
	}
	root, err := fs.Sub(profiles.FS, name)
	if err != nil {
		return Profile{}, unknown(name)
	}
	if _, err := fs.Stat(root, "profile.toml"); err != nil {
		return Profile{}, unknown(name)
	}
	return load(root, name, name+"/profile.toml")
}

// Load returns the profile called name for the project at dir: its custom
// profile in .crew/profiles/<name>/ if it has one, or else the built-in
// one. A custom profile may not take a built-in profile's name.
func Load(dir, name string) (Profile, error) {
	if !validName(name) {
		return Profile{}, unknown(name)
	}
	rel := Dir + "/" + name
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel), "profile.toml"))
	if errors.Is(err, fs.ErrNotExist) {
		return Builtin(name)
	}
	if err != nil {
		return Profile{}, err
	}
	if _, err := Builtin(name); err == nil {
		return Profile{}, fmt.Errorf("%s: %s is a built-in profile; rename the custom one", rel, name)
	}
	p, err := load(os.DirFS(filepath.Join(dir, filepath.FromSlash(rel))), name, rel+"/profile.toml")
	p.Custom = rel
	return p, err
}

func validName(name string) bool { return name != "" && !strings.ContainsAny(name, `/\.`) }

// ErrUnknown is the error for a profile that does not exist.
var ErrUnknown = errors.New("unknown profile")

func unknown(name string) error { return fmt.Errorf("%w: %s", ErrUnknown, name) }

// load reads the profile in root, whose profile.toml is called file in
// errors. A missing docs/ or scaffold/ folder is an empty one.
func load(root fs.FS, name, file string) (Profile, error) {
	b, err := fs.ReadFile(root, "profile.toml")
	if err != nil {
		return Profile{}, err
	}
	p, err := Parse(file, b)
	if err != nil {
		return Profile{}, err
	}
	p.Name = name
	p.Docs, p.Scaffold = sub(root, "docs"), sub(root, "scaffold")
	return p, nil
}

// sub is the folder dir of root, or nil if there is none.
func sub(root fs.FS, dir string) fs.FS {
	if info, err := fs.Stat(root, dir); err != nil || !info.IsDir() {
		return nil
	}
	s, _ := fs.Sub(root, dir)
	return s
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
