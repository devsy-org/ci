package main

import (
	"os"

	"github.com/devsy-org/ci/internal/baseline"
)

func main() {
	os.Exit(baseline.Run(os.Args[1:], os.Stdout, os.Stderr))
}
