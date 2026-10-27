# gha-concurrency-audit

[![ci](https://github.com/moveeeax/gha-concurrency-audit/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/gha-concurrency-audit/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

Finds the GitHub Actions workflows in a repository that can race a deploy against
itself, or that keep burning runner minutes on pull-request runs nobody will read.
Every finding comes with the `concurrency` block to paste.

GitHub runs every trigger it receives, in parallel, unless you tell it not to. Two
pushes to `main` mean two `terraform apply` runs, and the final state is whichever
finished last rather than whichever commit is newer. Ten fixup pushes mean ten full
matrix builds. The fix is four lines of YAML per workflow, and nothing in the
toolchain tells you which of your thirty workflows is missing them.

## Install

```sh
go install github.com/moveeeax/gha-concurrency-audit/cmd/gha-concurrency-audit@latest
```

Or build from a checkout:

```sh
go build -o gha-concurrency-audit ./cmd/gha-concurrency-audit
```

## Usage

```sh
gha-concurrency-audit                                            # scan ./.github/workflows
gha-concurrency-audit --dir ../infra --format markdown --fail-on high
gha-concurrency-audit --format json --ignore GROUP_TOO_FINE
```

```console
$ gha-concurrency-audit --dir testdata/messy --fail-on high
SEVERITY  RULE                        CLASS     WORKFLOW                                             NOTE
high      DEPLOY_CANCEL_IN_PROGRESS   deploy    testdata/messy/.github/workflows/deploy-staging.yml  cancel-in-progress is true; cancelling mid-apply leaves half-applied state, queue instead
medium    GROUP_TOO_COARSE            pr-check  testdata/messy/.github/workflows/integration.yml     group "${{ github.workflow }}" has no ref, head_ref or environment interpolation, so unrelated refs serialise behind one queue
medium    MISSING_PR_CANCEL           pr-check  testdata/messy/.github/workflows/pr-matrix.yml       superseded pushes keep running: ~45 runner-min/week (3 jobs x 5 min x 3 superseded pushes/week, estimated)
high      MISSING_DEPLOY_CONCURRENCY  deploy    testdata/messy/.github/workflows/release.yml         deploy workflow has no concurrency block, so two pushes can apply at once and the slower run wins
low       GROUP_TOO_FINE              other     testdata/messy/.github/workflows/sha-keyed.yml       group "build-${{ github.sha }}" is unique per run, so it can never collide and the block does nothing

5 workflow(s), 5 finding(s): 2 high, 2 medium, 1 low

Suggested blocks

testdata/messy/.github/workflows/release.yml (deploy)
  concurrency:
    group: ${{ github.workflow }}-${{ github.ref }}
    cancel-in-progress: false
```

Full output for the clean and messy fixtures, including the JSON shape, is in
[`examples/README.md`](examples/README.md).

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--dir` | `.` | Repository root; workflows are read from `<dir>/.github/workflows` |
| `--format` | `table` | `table`, `json` or `markdown` (markdown is job-summary ready) |
| `--fail-on` | `high` | Exit 1 at this severity or above: `high`, `medium`, `low`, `never` |
| `--ignore` | — | Comma-separated rule names to skip; an unknown name is an error, not a no-op |
| `--version` | — | Print the version |

Exit codes: `0` clean, `1` a finding at or above `--fail-on`, `2` a usage error or an
unparseable workflow.

## How it works

Classification comes first, because "correct concurrency" is not one answer. A
workflow is a **deploy** if any job declares an `environment:` or runs something that
writes outside the runner (`terraform apply`, `kubectl rollout`, `helm upgrade`,
`npm publish`, `docker push`). It is a **pr-check** if `pull_request` triggers it, a
**scheduled** job if only `schedule` does, and **other** otherwise. A deploy wants to
queue; a pull-request check wants to cancel. The class decides which rule applies and
what the suggested block looks like.

| Rule | Severity | Fires when |
| --- | --- | --- |
| `MISSING_DEPLOY_CONCURRENCY` | high | A deploy workflow has no `concurrency` at all, at either level |
| `DEPLOY_CANCEL_IN_PROGRESS` | high | A deploy cancels mid-run, so an interrupted apply leaves half-applied state |
| `MISSING_PR_CANCEL` | medium | A `pull_request` workflow does not cancel superseded runs, with an estimate of the waste |
| `GROUP_TOO_COARSE` | medium | The group has no `ref`/`head_ref`/`environment` interpolation, so unrelated branches queue behind each other |
| `GROUP_TOO_FINE` | low | The group contains `github.sha` or `github.run_id`, so it can never collide and the block is decorative |

Workflow YAML needs a real parser rather than a regex pass: `on:` is a string, a
sequence or a mapping depending on who wrote the file, `concurrency` is a string or an
object, and `environment` is a name or an object. Parsing goes through `yaml.Node` and
matches mapping keys on their raw text, because YAML resolvers are entitled to turn the
key `on` into the boolean `true` — which is exactly how a naive struct decode loses
every trigger in the file.

Job-level `concurrency` counts: if the deploy job itself is guarded, the workflow is
fine even with nothing at the top. And `cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}`
is read as a deliberate choice to cancel, not as a missing setting.

## As a GitHub Action

```yaml
- uses: moveeeax/gha-concurrency-audit@v0
  with:
    fail-on: high
```

The composite action builds the auditor, writes the markdown table to
`$GITHUB_STEP_SUMMARY`, and exposes `exit-code` and `report` outputs. This repository
runs it on itself in CI.

## Tests

```sh
go test ./...
```

The suite runs against five fixture repositories under `testdata/` — one correctly
configured, one with each finding, one covering the awkward YAML shapes, one with
broken YAML, one with no workflows at all — plus a test that audits this repository
and requires zero findings.

## One caveat about the estimate

`MISSING_PR_CANCEL` prints "~45 runner-min/week" and similar. That number is
`jobs × 5 min × 3 superseded pushes/week`, all three assumed. It is there for order of
magnitude, not for a budget line. Measuring it properly means reading run history from
the Actions API, which is the first thing on the stretch list.

## License

MIT
