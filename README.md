# Devsy CI tools

This repo holds shared CI tools for Devsy repositories.

## PR size

The `pr-size` action counts a pull request's changed lines by class and fails
when production lines exceed the limit.

Run it in its own job on `pull_request` with the types `opened`,
`synchronize`, `reopened`, `labeled`, `unlabeled` and `edited`, so the
verdict updates when the label or PR body changes. Check out the caller repo
with `fetch-depth: 0` first; the action does not fetch refs. It needs git
2.41 or later. Replace `<sha>` with the commit SHA to pin.

```yaml
on:
  pull_request:
    types: [opened, synchronize, reopened, labeled, unlabeled, edited]
jobs:
  pr-size:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@<sha>
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: devsy-org/ci/pr-size@<sha>
        with:
          limit: "400"
          report-only: "false"
```

Keep the job free of secrets and of steps that run code from the PR
checkout. Do not switch to `pull_request_target` to check fork PRs: that
event can have a write token and access to secrets. The action installs its own Go
version and sets `GOTOOLCHAIN=local` for later steps in the same job, which
is one more reason to give it a dedicated job.

The action compares `HEAD` with its merge base on `origin/<base-ref>`. By
default, `base-ref` is the PR target branch. Git attributes are read from the
merge base, so `.gitattributes` changes in the PR do not change the count.

The report groups changed files as generated, tests, docs, config, or
production. Only production additions and removals count toward the limit.
An edited rename counts as production when either path is production.
Whole-file deletions and unchanged renames count as zero lines. Files Git
reports as binary count as zero when they have a known binary extension;
otherwise they are counted from a text diff. The job summary lists the
binary files that were not counted, so reviewers can check them. The job
log shows only the table and the verdict.

To exceed the limit, add the `size-exception` label and a line in the PR body
starting with `Size exception:`, followed by a reason of at least 20
printable characters after trimming spaces. Set `report-only` to `"true"` to
show the verdict without enforcing it.

## Lint baseline

The `lint-baseline` action runs golangci-lint and fails when a pull request
adds findings or leaves stale entries in its baseline. Run it in its own
job on `pull_request` with a read-only token and no secrets. Check out the
caller repo at the default PR merge ref with `fetch-depth: 0`; the action
does not fetch refs.
It needs Git 2.41 or later. Replace `<sha>` with the commit SHA to pin.

```yaml
on:
  pull_request:
jobs:
  lint-baseline:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: devsy-org/ci/lint-baseline@<sha>
```

The action checks that `HEAD` includes `origin/<base-ref>` and compares
it with that merge base. Both sides need a `go.mod`; lint uses the highest
Go version named by their `go` and `toolchain` directives.
It replaces the checkout's `.golangci.yaml` with the merge base copy
before linting. The config must already exist at the merge base. If the
base has no `.lint-baseline.tsv`, the PR can add one that matches the
current findings. Once the file is on the base branch, later PRs can only
shrink it. The action rejects added bare `//nolint` directives and ones
that suppress guarded linters. The defaults are `cyclop`, `funlen`,
`gocognit`, `gocyclo`, `nestif`, `maintidx` and `revive`.
It also rejects new Go generated-code headers because golangci-lint
skips those files by default. Set `allow-generated: "true"` to allow them.
New `go.mod` and `go.work` files need `allow-new-modules: "true"`.
The action also rejects new line directives and build constraints that
mention the `ignore` tag.

The full-tree check fails on any `unused` or `unparam` finding; set
`dead-code: "false"` while the baseline still lists these findings, since
the baseline check already rejects new ones. Run the same command on
pushes to `main`, since the action needs a PR merge base:

```sh
golangci-lint run --config .golangci.yaml --enable-only unused,unparam \
  --allow-parallel-runners --path-prefix=./ ./...
```

Set `working-directory` for a Go module outside the repo root. Set `config`
and `baseline` to paths relative to that directory to use different names.
Use `golangci-lint-version` to override `v2.15.0` or `guarded-linters` to
override the comma-separated list. Do not run this job on
`pull_request_target`, which can have a write token and access to secrets.
