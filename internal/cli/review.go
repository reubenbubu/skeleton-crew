package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/reubenbubu/skeleton-crew/internal/output"
	"github.com/reubenbubu/skeleton-crew/internal/ticket"
)

func runReview(args []string, env Env) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	approve := fs.Bool("approve", false, "")
	reason := fs.String("reject", "", "")
	asJSON := fs.Bool("json", false, "")
	pos, err := positional(fs, args, "<n>")
	var n int
	if err == nil {
		n, err = number(pos[0])
	}
	rejected := false
	fs.Visit(func(f *flag.Flag) { rejected = rejected || f.Name == "reject" })
	switch {
	case err != nil:
	case *approve && rejected:
		err = usageError{"only one of --approve and --reject"}
	case !*approve && !rejected:
		err = usageError{"missing flag: --approve or --reject"}
	case rejected && strings.TrimSpace(*reason) == "":
		err = usageError{"missing reason: --reject"}
	}
	if err != nil {
		return finish(env, err, false)
	}
	c, err := client(env)
	if err != nil {
		return finish(env, err, false)
	}
	pr, err := ticket.Review(c, n, *reason)
	if err != nil {
		return finish(env, err, false)
	}
	stage := "5:review-approved"
	if rejected {
		stage = "4:review-rejected"
	}
	if *asJSON {
		err = output.JSON(env.Stdout, struct {
			Number int    `json:"number"`
			PR     int    `json:"pr"`
			Stage  string `json:"stage"`
		}{n, pr, stage})
	} else {
		err = lines(env.Stdout, fmt.Sprintf("#%d stage=%s", n, stage))
	}
	return finish(env, err, false)
}
