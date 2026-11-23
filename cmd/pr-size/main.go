package main

import (
	"context"
	"flag"
	"os"

	"github.com/devsy-org/ci/internal/prsize"
)

func main() {
	var opts prsize.Options
	flag.StringVar(&opts.BaseRef, "base-ref", "", "target branch name on origin")
	flag.IntVar(&opts.Limit, "limit", 400, "maximum changed production lines")
	flag.BoolVar(&opts.ReportOnly, "report-only", false, "report without enforcing the verdict")
	flag.StringVar(&opts.Repo, "repo", ".", "path to the pull request checkout")
	flag.Parse()
	os.Exit(prsize.Run(context.Background(), opts, flag.NArg()))
}
