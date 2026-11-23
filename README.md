# Devsy CI tools

This repo holds shared CI tools for Devsy repositories.

## PR size

The `pr-size` action counts a pull request's changed lines by class and fails
when production lines exceed the limit.

Run it in a `pull_request` workflow with the types `opened`, `synchronize`,
`reopened`, `labeled`, `unlabeled` and `edited`, so the verdict updates when
the label or PR body changes. Check out the caller repo with `fetch-depth: 0`
first; the action does not fetch refs. It needs git 2.40 or later. Replace
`<sha>` with the commit SHA to pin.

```yaml
- uses: actions/checkout@<sha>
  with:
    fetch-depth: 0
    persist-credentials: false
- uses: devsy-org/ci/pr-size@<sha>
  with:
    limit: "400"
    report-only: "false"
```

The action compares `HEAD` with its merge base on `origin/<base-ref>`. By
default, `base-ref` is the PR target branch. Git attributes are read from the
merge base, so `.gitattributes` changes in the PR do not change the count.

The report groups changed files as generated, tests, docs, config, or
production. Only production additions and removals count toward the limit.
An edited rename counts as production when either path is production.
Whole-file deletions, unchanged renames and files with a known binary
extension count as zero lines. Other files Git treats as binary are counted
from a text diff.

To exceed the limit, add the `size-exception` label and a line in the PR body
starting with `Size exception:`. Give a reason of at least 20 characters.
Set `report-only` to `"true"` to show the verdict without enforcing it.
