package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
	"github.com/reubenbubu/skeleton-crew/internal/toml"
)

// The main instance runs the app at the head of main, at the host ports
// in portsFile, unless mainOff exists.
var (
	mainOff      = filepath.Join(".crew", "main.off")
	portsFile    = filepath.Join(".crew", "ports.toml")
	mainWorktree = filepath.Join(".crew", "worktrees", "main")
	portsYAML    = filepath.Join(".crew", "worktrees", "main.ports.yaml")
)

// ports maps a compose service to its container ports, each with its host
// port.
type ports map[string]map[int]int

// serviceName is a compose service name that portsFile can hold as a
// table.
var serviceName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func runMain(args []string, env Env) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return finish(env, usageError{"usage: crew main up|down|logs"}, false)
	}
	fs := flag.NewFlagSet("main "+args[0], flag.ContinueOnError)
	volumes := new(bool)
	switch args[0] {
	case "up", "logs":
	case "down":
		volumes = fs.Bool("volumes", false, "")
	default:
		return finish(env, usageError{"unknown command: main " + args[0]}, false)
	}
	if err := flags(fs, args[1:]); err != nil {
		return finish(env, err, false)
	}
	r, err := root(env)
	if err != nil {
		return finish(env, err, false)
	}
	_, prof, err := loadConfig(r)
	if err != nil {
		return finish(env, err, false)
	}
	if _, ok := prof.Commands["run"]; !ok {
		return finish(env, fmt.Errorf("no main instance: the %s profile has no run command", prof.Name), false)
	}
	project := compose.MainProject(r)
	dir := filepath.Join(r, mainWorktree)
	if _, err := os.Stat(dir); err != nil {
		dir = r
	}
	switch args[0] {
	case "up":
		if err := os.Remove(filepath.Join(r, mainOff)); err != nil && !os.IsNotExist(err) {
			return finish(env, err, false)
		}
		sha, err := (&git.Repo{Dir: r}).Branch("main")
		if err != nil {
			return finish(env, err, false)
		}
		out, err := mainUp(env, r, sha)
		if err == nil {
			err = lines(env.Stdout, out...)
		}
		return finish(env, err, false)
	case "down":
		// Turned off first, so merges leave it stopped even when the
		// down fails.
		if err := os.WriteFile(filepath.Join(r, mainOff), nil, 0o644); err != nil {
			return finish(env, err, false)
		}
		a := []string{"-p", project, "down", "--remove-orphans"}
		if *volumes {
			a = append(a, "--volumes")
		}
		_, err := composeOK(env, dir, "down", a...)
		if err == nil {
			err = lines(env.Stdout, "main down")
		}
		return finish(env, err, false)
	}
	out, err := composeOK(env, dir, "logs", "-p", project, "logs", "--no-color")
	if err == nil {
		_, err = env.Stdout.Write(out)
	}
	return finish(env, err, false)
}

// refreshMain refreshes the main instance at commit sha after a merge,
// unless the profile has no run command or the instance is turned off. A
// failure is a warning on stderr: the merge stands.
func refreshMain(env Env, root string, prof profile.Profile, sha string) []string {
	if _, ok := prof.Commands["run"]; !ok {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, mainOff)); err == nil {
		return nil
	}
	out, err := mainUp(env, root, sha)
	if err != nil {
		fmt.Fprintf(env.Stderr, "warning: main instance not refreshed: %s; run crew main up\n", err)
		return nil
	}
	return out
}

// mainUp moves the main instance's worktree to commit sha and starts the
// instance there, at the ports in portsFile. When portsFile does not exist
// it first starts the instance at the ports the container engine picks,
// and writes each published port to portsFile with the same host port.
func mainUp(env Env, root, sha string) ([]string, error) {
	dir := filepath.Join(root, mainWorktree)
	if err := (&git.Repo{Dir: root}).MoveDetached(dir, sha); err != nil {
		return nil, err
	}
	project := compose.MainProject(root)
	up := []string{"up", "-d", "--build", "--force-recreate", "--renew-anon-volumes"}
	fixed, err := readPorts(filepath.Join(root, portsFile))
	if errors.Is(err, os.ErrNotExist) {
		if _, err := composeOK(env, dir, "up", append([]string{"-p", project}, up...)...); err != nil {
			return nil, err
		}
		picked, err := published(env, dir, project)
		if err != nil {
			return nil, err
		}
		fixed = ports{}
		for s, ps := range picked {
			if !serviceName.MatchString(s) {
				continue
			}
			fixed[s] = map[int]int{}
			for c := range ps {
				fixed[s][c] = c
			}
		}
		if err := os.WriteFile(filepath.Join(root, portsFile), portsTOML(fixed), 0o644); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	a := []string{"-p", project}
	if len(fixed) > 0 {
		if err := os.WriteFile(filepath.Join(root, portsYAML), overrideYAML(fixed), 0o644); err != nil {
			return nil, err
		}
		a = append(a, "-f", "compose.yaml", "-f", filepath.Join(root, portsYAML))
	}
	if _, err := composeOK(env, dir, "up", append(a, up...)...); err != nil {
		return nil, err
	}
	now, err := published(env, dir, project)
	if err != nil {
		return nil, err
	}
	out := []string{"main " + sha[:7] + " up"}
	for _, s := range slices.Sorted(maps.Keys(now)) {
		for _, c := range slices.Sorted(maps.Keys(now[s])) {
			h := now[s][c]
			out = append(out, fmt.Sprintf("%s %d 127.0.0.1:%d", s, c, h))
		}
	}
	return out, nil
}

// composeOK runs compose with args in dir. A non-zero exit prints the end
// of the output on stderr and is an error named after step.
func composeOK(env Env, dir, step string, args ...string) ([]byte, error) {
	out, code, err := env.Compose(dir, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		env.Stderr.Write([]byte(tail(out, 20)))
		return nil, fmt.Errorf("%s: exit %d", step, code)
	}
	return out, nil
}

// published returns the host port of each container port that project's
// running services publish, from compose ps. It reads both Docker
// Compose's JSON, one object per container with Publishers, and Podman's,
// an array of containers with Ports and a service label.
func published(env Env, dir, project string) (ports, error) {
	out, err := composeOK(env, dir, "ps", "-p", project, "ps", "--format", "json")
	if err != nil {
		return nil, err
	}
	type entry struct {
		Service    string
		Labels     json.RawMessage
		Publishers []struct{ TargetPort, PublishedPort int }
		Ports      json.RawMessage
	}
	var entries []entry
	if i := bytes.IndexAny(out, "[{"); i >= 0 {
		d := json.NewDecoder(bytes.NewReader(out[i:]))
		for {
			var raw json.RawMessage
			if err := d.Decode(&raw); err == io.EOF {
				break
			} else if err != nil {
				return nil, fmt.Errorf("ps: unexpected output: %s", err)
			}
			var es []entry
			if raw[0] == '[' {
				err = json.Unmarshal(raw, &es)
			} else {
				es = make([]entry, 1)
				err = json.Unmarshal(raw, &es[0])
			}
			if err != nil {
				return nil, fmt.Errorf("ps: unexpected output: %s", err)
			}
			entries = append(entries, es...)
		}
	}
	got := ports{}
	add := func(s string, c, h int) {
		if s == "" || c <= 0 || h <= 0 {
			return
		}
		if got[s] == nil {
			got[s] = map[int]int{}
		}
		got[s][c] = h
	}
	for _, e := range entries {
		for _, p := range e.Publishers {
			add(e.Service, p.TargetPort, p.PublishedPort)
		}
		var labels map[string]string
		var podman []struct {
			ContainerPort int `json:"container_port"`
			HostPort      int `json:"host_port"`
		}
		if json.Unmarshal(e.Labels, &labels) != nil || json.Unmarshal(e.Ports, &podman) != nil {
			continue
		}
		for _, p := range podman {
			add(labels["com.docker.compose.service"], p.ContainerPort, p.HostPort)
		}
	}
	return got, nil
}

// readPorts reads portsFile at name.
func readPorts(name string) (ports, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	rel := filepath.ToSlash(portsFile)
	doc, err := toml.Parse(rel, b)
	if err != nil {
		return nil, err
	}
	p := ports{}
	for _, t := range doc.Tables {
		if strings.Contains(t, ".") {
			return nil, &toml.Error{File: rel, Line: doc.TableLines[t], Msg: "unsupported table " + t}
		}
		p[t] = map[int]int{}
	}
	for k, v := range doc.Values {
		s, c, _ := strings.Cut(k, ".")
		container, err := strconv.Atoi(c)
		if p[s] == nil || err != nil || container < 1 || container > 65535 {
			return nil, &toml.Error{File: rel, Line: v.Line, Msg: "unsupported key"}
		}
		if v.Kind != toml.Int || v.Int < 1 || v.Int > 65535 {
			return nil, &toml.Error{File: rel, Line: v.Line, Msg: "not a port"}
		}
		p[s][container] = int(v.Int)
	}
	return p, nil
}

// portsTOML is portsFile holding p.
func portsTOML(p ports) []byte {
	b := []byte("# Host ports of the main instance. crew writes this file once and never changes it.\n")
	for _, s := range slices.Sorted(maps.Keys(p)) {
		b = fmt.Appendf(b, "\n[%s]\n", s)
		for _, c := range slices.Sorted(maps.Keys(p[s])) {
			b = fmt.Appendf(b, "%d = %d\n", c, p[s][c])
		}
	}
	return b
}

// overrideYAML is a compose file that replaces the ports of each service
// in p with its fixed host ports on 127.0.0.1.
func overrideYAML(p ports) []byte {
	b := []byte("services:\n")
	for _, s := range slices.Sorted(maps.Keys(p)) {
		b = fmt.Appendf(b, "  %q:\n    ports: !override\n", s)
		for _, c := range slices.Sorted(maps.Keys(p[s])) {
			b = fmt.Appendf(b, "      - \"127.0.0.1:%d:%d\"\n", p[s][c], c)
		}
		if len(p[s]) == 0 {
			b = b[:len(b)-1]
			b = append(b, " []\n"...)
		}
	}
	return b
}
