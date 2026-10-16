// Command crew is the Skeleton Crew CLI.
package main

import (
	"fmt"
	"os"

	"github.com/reubenbubu/skeleton-crew/internal/cli"
	"github.com/reubenbubu/skeleton-crew/internal/clock"
	"github.com/reubenbubu/skeleton-crew/internal/doctor"
	"github.com/reubenbubu/skeleton-crew/internal/github"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		Version: version,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Clock:   clock.Real{},
		Dir:     dir,
		Getenv:  os.Getenv,
		GH:      github.Default(),
		Probe:   doctor.Exec,
	}))
}
