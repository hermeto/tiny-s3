# Contract: CI workflow and checks

The workflow is `.github/workflows/ci.yml`, named `CI`. Decisions are in [research.md](../research.md).
Branch rulesets, spec 014 and contributors rely on the check names and tags below, so changing them is a
breaking change that needs the ruleset updated in the same pull request.

## Triggers

| Event | Filter | Runs |
|-------|--------|------|
| `push` | `branches: ['**']` (no tags) | `gate` ×4, `image`; plus `publish` when the ref is `refs/heads/main` |
| `pull_request` | `branches: [main]`; opened, reopened, synchronize | `gate` ×4, `image` |

No other trigger. `pull_request_target` and `workflow_run` are never used.

## Checks

| Check name (job name) | Job id | Runs | Reproduce locally | Fails when |
|-----------------------|--------|------|-------------------|------------|
| `make vet` | `gate` (matrix `vet`) | every run | `make vet` | `go vet -tags e2e ./...` reports anything |
| `make test` | `gate` (matrix `test`) | every run | `make test` | any test fails |
| `make race` | `gate` (matrix `race`) | every run | `make race` | any test fails or a race is reported |
| `make cover` | `gate` (matrix `cover`) | every run | `make cover` | any `internal/` package below 100% statements or without tests |
| `docker build` | `image` | every run | `docker build .` | the Dockerfile does not build |
| `publish` | `publish` | push to `main` only | n/a | login, build or push to GHCR fails |

The five checks other than `publish` are the required checks for `main` (spec FR-015). On a pull request each
appears twice, once per event, e.g. `CI / make vet (push)` and `CI / make vet (pull_request)`; the ruleset
matches the job name, so both count.

Gates are independent (`fail-fast: false`, no `needs` between them). `publish` has `needs: [gate, image]`, so it
is skipped unless all five pass.

## Concurrency

```yaml
group: ci-${{ github.event_name }}-${{ github.ref }}
cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}
```

- Branch push or pull request: a newer run cancels the older one in the same group.
- `main`: runs are never cancelled; at most one runs and one waits, and a newer waiting run supersedes an older
  waiting one.

## Permissions

| Scope | `contents` | `packages` | Anything else |
|-------|-----------|------------|---------------|
| Workflow default | read | none | none |
| `publish` | read | write | none |

`actions/checkout` runs with `persist-credentials: false` in every job.

## Published image

| Property | Value |
|----------|-------|
| Repository | `ghcr.io/hermeto/tiny-s3` (lower-cased `github.repository`) |
| Tags | full 40-character commit SHA; `latest` |
| Platform | `linux/amd64` |
| Labels | OCI labels from `docker/metadata-action`, including `org.opencontainers.image.source` and `org.opencontainers.image.revision` |
| Published when | push to `main`, all five checks passed |

Both tags of one run point to the same digest. Re-running a `main` run succeeds and moves the SHA tag and
`latest` together; the digest may change because provenance metadata includes build timestamps, while the image
layers stay the same.

## Timeouts

`gate` 15 minutes, `image` 15 minutes, `publish` 20 minutes.
