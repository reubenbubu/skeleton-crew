package cli

import (
	"errors"
	"flag"
	"os"
	"path/filepath"

	"github.com/reubenbubu/skeleton-crew/internal/config"
	"github.com/reubenbubu/skeleton-crew/internal/git"
	"github.com/reubenbubu/skeleton-crew/internal/github"
	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/upgrade"
)

// upgraded is what crew upgrade did: the version pinned before and after,
// and the install method the configuration records.
type upgraded struct {
	Previous string `json:"previous"`
	Pinned   string `json:"pinned"`
	Install  string `json:"install"`
	Commit   string `json:"commit"` // "" when nothing was committed
}

func runUpgrade(args []string, env Env) int {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	if err := flags(fs, args); err != nil {
		return finish(env, err, false)
	}
	res, err := moveUpgradePin(env)
	if err != nil {
		return finish(env, err, false)
	}
	switch {
	case *asJSON:
		err = output.JSON(env.Stdout, res)
	case res.Pinned == res.Previous:
		err = lines(env.Stdout, "up to date "+res.Pinned)
	default:
		out := []string{"pinned " + res.Pinned + " was " + res.Previous}
		if res.Commit != "" {
			out = append(out, "commit "+res.Commit[:min(7, len(res.Commit))]+" pushed main")
		}
		out = append(out, "install "+res.Pinned+" with install="+res.Install+" as the bootstrap in "+upgrade.File+" says")
		err = lines(env.Stdout, out...)
	}
	return finish(env, err, false)
}

// moveUpgradePin moves the version AGENTS.md pins to the latest release. It
// only moves the pin forwards.
func moveUpgradePin(env Env) (upgraded, error) {
	r, err := root(env)
	if err != nil {
		return upgraded{}, err
	}
	// Only the install method is needed, so a profile that no longer loads
	// does not stop an upgrade.
	b, err := os.ReadFile(filepath.Join(r, config.Path))
	if errors.Is(err, os.ErrNotExist) {
		return upgraded{}, errors.New("no " + config.Path + "; run crew init")
	} else if err != nil {
		return upgraded{}, err
	}
	cfg, err := config.Parse(config.Path, b)
	if err != nil {
		return upgraded{}, err
	}
	res := upgraded{Install: cfg.Install}
	move := func(old []byte) ([]byte, string, error) {
		pinned, err := upgrade.Pinned(old)
		if err != nil {
			return nil, "", err
		}
		res.Previous, res.Pinned = pinned, pinned
		latest, err := github.New(upgrade.Source, env.GH).LatestRelease()
		if err != nil {
			return nil, "", err
		}
		newer, err := upgrade.Newer(latest.TagName, pinned)
		if err != nil || !newer {
			return old, "", err
		}
		b, err := upgrade.Pin(old, latest.TagName)
		res.Pinned = latest.TagName
		return b, "Upgrade crew to " + latest.TagName, err
	}
	// AGENTS.md is the user's file. Only when they track it does the new pin
	// go to main; otherwise it changes in place.
	_, tracked, err := (&git.Repo{Dir: r}).Read("HEAD", upgrade.File)
	if err != nil {
		return upgraded{}, err
	}
	if tracked {
		res.Commit, err = commitMain(r, upgrade.File, nil, move)
		return res, err
	}
	path := filepath.Join(r, upgrade.File)
	old, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return upgraded{}, upgrade.ErrNoPin
	} else if err != nil {
		return upgraded{}, err
	}
	moved, _, err := move(old)
	if err != nil {
		return upgraded{}, err
	}
	if res.Pinned != res.Previous {
		err = os.WriteFile(path, moved, 0o644)
	}
	return res, err
}
