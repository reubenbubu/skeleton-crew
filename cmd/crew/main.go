// Command crew is the Skeleton Crew CLI.
package main

import (
	"os"

	"github.com/reubenbubu/skeleton-crew/internal/cli"
	"github.com/reubenbubu/skeleton-crew/internal/clock"
)

// version is set at build time with -ldflags "-X main.version=<version>".
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		Version: version,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Clock:   clock.Real{},
	}))
}
