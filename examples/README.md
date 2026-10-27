# Examples

Two fixture repositories live under [`testdata/`](../testdata): `clean` is configured
the way I would want it, `messy` has one of each finding. Both outputs below are real,
copied from a local run.

## A repository that passes

```console
$ gha-concurrency-audit --dir testdata/clean --fail-on low
3 workflow(s) scanned, no concurrency findings.
# exit 0
```

## A repository that does not

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

testdata/messy/.github/workflows/deploy-staging.yml (deploy)
  concurrency:
    group: deploy-staging
    cancel-in-progress: false

testdata/messy/.github/workflows/integration.yml (pr-check)
  concurrency:
    group: ${{ github.workflow }}-${{ github.head_ref || github.ref }}
    cancel-in-progress: true

testdata/messy/.github/workflows/pr-matrix.yml (pr-check)
  concurrency:
    group: ${{ github.workflow }}-${{ github.head_ref || github.ref }}
    cancel-in-progress: true

testdata/messy/.github/workflows/release.yml (deploy)
  concurrency:
    group: ${{ github.workflow }}-${{ github.ref }}
    cancel-in-progress: false

testdata/messy/.github/workflows/sha-keyed.yml (other)
  concurrency:
    group: ${{ github.workflow }}-${{ github.ref }}
    cancel-in-progress: false
# exit 1
```

## Only the high findings, as JSON

```console
$ gha-concurrency-audit --dir testdata/messy --format json --ignore GROUP_TOO_FINE,GROUP_TOO_COARSE,MISSING_PR_CANCEL
{
  "dir": "testdata/messy",
  "workflows": [
    {
      "path": "testdata/messy/.github/workflows/deploy-staging.yml",
      "name": "deploy-staging",
      "class": "deploy",
      "events": [
        "push"
      ],
      "concurrency": "deploy-${{ github.ref }} [cancels]"
    },
    {
      "path": "testdata/messy/.github/workflows/integration.yml",
      "name": "integration",
      "class": "pr-check",
      "events": [
        "push",
        "pull_request"
      ],
      "concurrency": "${{ github.workflow }} [cancels]"
    },
    {
      "path": "testdata/messy/.github/workflows/pr-matrix.yml",
      "name": "pr-matrix",
      "class": "pr-check",
      "events": [
        "pull_request"
      ],
      "concurrency": "none"
    },
    {
      "path": "testdata/messy/.github/workflows/release.yml",
      "name": "release",
      "class": "deploy",
      "events": [
        "push"
      ],
      "concurrency": "none"
    },
    {
      "path": "testdata/messy/.github/workflows/sha-keyed.yml",
      "name": "sha-keyed",
      "class": "other",
      "events": [
        "push"
      ],
      "concurrency": "build-${{ github.sha }} [cancels]"
    }
  ],
  "findings": [
    {
      "workflow": "testdata/messy/.github/workflows/deploy-staging.yml",
      "name": "deploy-staging",
      "class": "deploy",
      "rule": "DEPLOY_CANCEL_IN_PROGRESS",
      "severity": "high",
      "note": "cancel-in-progress is true; cancelling mid-apply leaves half-applied state, queue instead",
      "suggestion": "concurrency:\n  group: deploy-staging\n  cancel-in-progress: false"
    },
```
