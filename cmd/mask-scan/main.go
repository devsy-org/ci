package main

import (
	"context"
	"flag"
	"os"

	"github.com/devsy-org/ci/internal/maskscan"
)

func main() {
	var opts maskscan.Options
	flag.StringVar(&opts.BaseRef, "base-ref", "", "target branch name on origin")
	flag.StringVar(&opts.Repo, "repo", ".", "path to the pull request checkout")
	flag.BoolVar(&opts.ReportOnly, "report-only", false, "report without enforcing the verdict")
	flag.Func(
		"allow",
		"exclude a glob or directory prefix (repeatable)",
		func(pattern string) error {
			opts.Allow = append(opts.Allow, pattern)
			return nil
		},
	)
	flag.Parse()
	os.Exit(maskscan.Run(context.Background(), opts, flag.NArg()))
}
