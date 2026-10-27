# gha-concurrency-audit
Audits .github/workflows for missing or wrong concurrency configuration: deploys that can race, PR checks that never cancel superseded runs, groups too coarse or too fine. Prints a ready-to-paste concurrency block per workflow and exits non-zero in CI.
