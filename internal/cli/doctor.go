package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/brief"
	"github.com/reubenbubu/skeleton-crew/internal/compose"
	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/doctor"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/profile"
)

func runDoctor(args []string, env Env) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	checks := diagnose(env, config.DetectHarness(env.Getenv))
	var err error
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Checks []doctor.Check `json:"checks"`
		}{checks})
	} else {
		for _, c := range checks {
			if _, err = fmt.Fprintln(env.Stdout, c); err != nil {
				break
			}
		}
	}
	return finish(env, err, doctor.Failed(checks))
}

// diagnose runs every check of crew doctor, in a fixed order, for harness,
// the harness in use, or "" when crew cannot tell. It stops after a
// failed configuration check, which every later check needs.
func diagnose(env Env, harness string) []doctor.Check {
	checks := doctor.Run(env.Probe, env.GH)
	engine := checks[1].Status == doctor.OK
	last := checks[len(checks)-1]
	authed := last.Name == doctor.Auth && last.Status == doctor.OK

	r, err := root(env)
	if err != nil {
		return append(checks, doctor.Failure("config", err.Error()))
	}
	cfg, prof, err := loadConfig(r)
	if err != nil {
		return append(checks, doctor.Failure("config", err.Error()))
	}
	checks = append(checks, doctor.Passed("config", cfg.Profile), doctor.Tiers(cfg))

	if !authed {
		checks = append(checks, doctor.Failure("labels", "not checked; needs "+doctor.Auth))
	} else if c, err := client(env); err != nil {
		checks = append(checks, doctor.Failure("labels", err.Error()))
	} else if existing, err := c.Labels(); err != nil {
		checks = append(checks, doctor.Failure("labels", err.Error()))
	} else {
		checks = append(checks, doctor.Labels(existing, cfg))
	}

	repo := &git.Repo{Dir: r}
	sha, docs, adrs, err := mainKnowledge(repo)
	if err != nil {
		checks = append(checks, doctor.Failure("knowledge", err.Error()), doctor.Failure("rules", "not checked; needs origin/main"), doctor.Harness(cfg, harness))
		return append(checks, doctor.Failure("gate", "not run; needs origin/main"))
	}
	checks = append(checks, doctor.Knowledge(docs))
	checks = append(checks, doctor.Conflicts(append(docs, adrs...), prof.Skip)...)
	checks = append(checks, doctor.Harness(cfg, harness))
	if !engine {
		return append(checks, doctor.Failure("gate", "not run; needs a container engine"))
	}
	return append(checks, doctorGate(env, r, repo, prof, sha))
}

// mainKnowledge reads the knowledge files that every briefing holds in
// full, and the ADRs, at origin/main, whose commit it returns.
func mainKnowledge(repo *git.Repo) (sha string, docs, adrs []doctor.File, err error) {
	if sha, err = repo.Branch("main"); err != nil {
		return "", nil, nil, err
	}
	for _, p := range knowledge {
		text, _, err := repo.Read(sha, p)
		if err != nil {
			return "", nil, nil, err
		}
		docs = append(docs, doctor.File{Path: p, Text: text})
	}
	files, err := repo.Files(sha)
	if err != nil {
		return "", nil, nil, err
	}
	for _, f := range files {
		if !strings.HasPrefix(f, brief.ADRDir+"/") {
			continue
		}
		text, _, err := repo.Read(sha, f)
		if err != nil {
			return "", nil, nil, err
		}
		adrs = append(adrs, doctor.File{Path: f, Text: text})
	}
	return sha, docs, adrs, nil
}

// doctorGate runs the gate's container steps once on commit sha, in a
// detached worktree at .crew/worktrees/gate/doctor. It posts no status:
// crew doctor only reports.
func doctorGate(env Env, root string, repo *git.Repo, prof profile.Profile, sha string) doctor.Check {
	dir := filepath.Join(root, ".crew", "worktrees", "gate", "doctor")
	if err := repo.AddDetached(dir, sha); err != nil {
		return doctor.Failure("gate", err.Error())
	}
	defer repo.RemoveWorktree(dir)
	var res gateResult
	if err := containerSteps(env, root, dir, compose.DoctorProject, "doctor", prof, &res); err != nil {
		return doctor.Failure("gate", err.Error())
	}
	if res.failed != "" {
		return doctor.Failure("gate", res.failed+"; full output: "+res.log)
	}
	return doctor.Passed("gate", sha[:7])
}
