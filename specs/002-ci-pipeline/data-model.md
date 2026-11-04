# Data Model: CI Pipeline

This feature has no application data. The entities below are the CI objects from the spec, with the attributes
and rules the workflow must keep. Names and values are fixed in [contracts/ci-checks.md](contracts/ci-checks.md).

## Workflow run

| Attribute | Value |
|-----------|-------|
| Event | `push` or `pull_request` |
| Ref | `refs/heads/<branch>` or `refs/pull/<n>/merge` |
| Commit | `github.sha` (for pull requests, the merge commit GitHub builds) |
| Concurrency group | `ci-<event>-<ref>` |

State transitions: `queued → in_progress → completed (success | failure | cancelled)`.
On non-`main` refs a newer run in the same group moves an in-progress run to `cancelled`. On `main` a newer
queued run moves an older *queued* run to `cancelled`; an in-progress `main` run always completes.

## Check

| Attribute | Rule |
|-----------|------|
| Name | One of `make vet`, `make test`, `make race`, `make cover`, `docker build`, `publish`; stable across runs |
| Result | `success`, `failure`, `cancelled`, `skipped` (`publish` off `main` or after a failed gate) |
| Log | Shows the exact `make` or build command that ran |

Rule: a gate's result depends only on the commit, never on cache state or on other gates.

## Cache entry

| Kind | Key | Restore key | Saved by |
|------|-----|-------------|----------|
| Go modules | `gomod-<os>-<hash(go.mod, go.sum)>` | none | successful gate runs on `main` |
| Go build cache | `gobuild-<os>-<target>-<sha>` | `gobuild-<os>-<target>-` | successful gate runs on `main` |
| BuildKit layers | `type=gha`, scope `image` | n/a | `image` job on `main` |

Rules: restored by every run that can read it (GitHub scoping: own ref plus the default branch); a missing or
evicted entry only makes the run slower; a dependency change produces a new module key.

## Published image

| Attribute | Rule |
|-----------|------|
| Name | `ghcr.io/<lower-cased owner/repo>` |
| Tags | `<full sha>` (immutable by convention, never rewritten to another commit), `latest` (moves forward only) |
| Source | the `main` commit whose five checks all passed |

Invariant: `latest` and `<sha>` of the newest published commit resolve to the same digest.
