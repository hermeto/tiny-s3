# Research: CI Pipeline

Each entry records a decision, why it was made, and what else was considered. Action release tags and
commit SHAs were resolved on 2026-11-04 from the GitHub API.

## R1. One workflow, triggers and runner

- **Decision**: A single workflow `.github/workflows/ci.yml` named `CI`, triggered by
  `push: branches: ['**']` and `pull_request: branches: [main]` (default activity types: opened, reopened,
  synchronize). Every job runs on `ubuntu-24.04`.
- **Rationale**: `branches: ['**']` matches every branch and excludes tag pushes, which the spec does not ask
  for. Pinning the runner image instead of `ubuntu-latest` keeps the environment explicit; a runner upgrade
  becomes a one-line, reviewed change. GitHub-hosted Linux runners ship Docker and GNU make, which is all the
  Makefile needs.
- **Alternatives considered**: separate workflows per gate (more files, and the publish job could not `needs`
  jobs in another workflow without `workflow_run`, which runs with elevated permissions in the base repository
  context); `ubuntu-latest` (silent environment changes).

## R2. Gates as one matrix job calling `make`

- **Decision**: One job `gate` with `strategy.matrix.target: [vet, test, race, cover]`,
  `strategy.fail-fast: false`, and `name: make ${{ matrix.target }}`. Its only real step is
  `make ${{ matrix.target }}`. The image is built in a separate job `image` named `docker build`.
- **Rationale**: The matrix produces four separate check runs named `make vet`, `make test`, `make race`,
  `make cover`. The name is the exact command to reproduce the failure locally (FR-005, SC-006, US1 scenario 6).
  `fail-fast: false` keeps one failing gate from cancelling the others (FR-006). Calling the Makefile, not
  `go test` directly, guarantees CI and local run the same image digest, flags and environment (FR-002, SC-002).
- **Alternatives considered**: four copy-pasted jobs (same result, four places to keep in sync); one job running
  all four targets in sequence (one check, failures hide behind each other, violates FR-005/FR-006); running Go
  directly on the runner with `actions/setup-go` (a second toolchain definition that can drift from the pinned
  container, which is exactly the disagreement the spec forbids).

## R3. Persisting Go caches between runs

- **Decision**: Make the two cache mounts in the Makefile overridable:

  ```make
  GOMOD_CACHE ?= tiny-s3-gomod
  GOBUILD_CACHE ?= tiny-s3-gobuild
  ```

  Locally nothing changes: the defaults are the existing named volumes. In CI the gate job sets them to
  absolute paths under `$RUNNER_TEMP`, which `docker run -v` treats as bind mounts, and `actions/cache` saves
  and restores those host directories.
  Cache keys:
  - module cache: `gomod-${{ runner.os }}-${{ hashFiles('go.mod', 'go.sum') }}`;
  - build cache: `gobuild-${{ runner.os }}-${{ matrix.target }}-${{ github.sha }}`, restore key
    `gobuild-${{ runner.os }}-${{ matrix.target }}-`.

  Every run restores; only runs from a push to `main` save (`actions/cache/restore` everywhere, plus
  `actions/cache/save` when `github.ref == 'refs/heads/main'`).
- **Rationale**: Bind-mounting host directories is the smallest change that lets the runner see the caches; the
  Makefile stays the single definition of how Go runs. Variable names are deliberately not `GOMODCACHE` or
  `GOCACHE`: make imports environment variables, so a developer with Go installed on the host would silently bind
  their host cache into the container. The build cache is keyed per target because `-race` and coverage builds
  produce different artifacts, and per SHA because the build cache changes on every commit while the restore key
  picks up the newest one. Saving only from `main` keeps entries few (one per target per `main` commit, oldest
  evicted by GitHub's LRU at 10 GB) and means a branch can never seed a cache that `main` later reads. GitHub
  already scopes caches by ref (a branch reads its own and the default branch's caches, never another branch's),
  so this is defence in depth, not the only control.
  The container runs as root, so restored and saved files are root-owned on the runner; Go makes cache files
  world-readable, so `actions/cache` (running as the `runner` user) can archive them.
- **Alternatives considered**: keep named volumes and `docker run` a helper container to tar/untar them (two more
  steps per job, the same result); caching the whole Docker data root (huge, slow); `actions/setup-go`'s built-in
  cache (requires host Go, see R2); saving caches from every branch (cache churn and eviction of `main`'s entries
  for little gain on short-lived branches).
- **Note**: With a warm build cache, `go test` may report `(cached)` for unchanged packages. That is the same
  behaviour developers get locally from the named volume, so it keeps CI and local equal (FR-002) rather than
  weakening the gate: Go's test cache key includes the test binary, its inputs and the environment.

## R4. Image build and publish

- **Decision**: The `image` job (`docker build`) uses `docker/setup-buildx-action` and
  `docker/build-push-action` with `push: false` and the GitHub Actions layer cache
  (`cache-from: type=gha,scope=image`, `cache-to: type=gha,mode=max,scope=image` only on `main`).
  A separate `publish` job runs only on `push` to `refs/heads/main`, `needs: [gate, image]`, logs in to
  `ghcr.io` with `GITHUB_TOKEN`, computes tags and OCI labels with `docker/metadata-action`
  (`type=raw,value=${{ github.sha }}` and `type=raw,value=latest`), and runs `docker/build-push-action` with
  `push: true` from the same layer cache.
- **Rationale**: Publishing in its own job is what lets only that job hold `packages: write` (FR-012), and
  `needs` on all gates means nothing is published unless all five pass (FR-009, US3 scenario 2). With a warm
  layer cache the second build is a cache hit, so the published layers are the ones built and checked by
  `docker build`. `docker/metadata-action` lower-cases the image name and adds
  `org.opencontainers.image.source`, which links the package to the repository so `GITHUB_TOKEN` can push to
  it. Tags use the full SHA as the spec requires, without the `sha-` prefix the action adds by default.
- **Alternatives considered**: build once, export a tarball as an artifact and push it from `publish`
  (byte-identical guarantee, but an upload/download round trip and a manual `docker load` + tag + push; worth
  revisiting if spec 014 needs to run e2e against the exact built image); building and pushing in the `image`
  job on `main` (it would need `packages: write` and would publish before the gates finish).
- **Platform**: `linux/amd64` only, the runner's native platform (spec Assumptions). The `golang` and
  distroless base images are pinned by digest of a multi-arch index, so adding `linux/arm64` later is a
  `platforms:` change plus QEMU.

## R5. Concurrency and the `main` ordering rule

- **Decision**:

  ```yaml
  concurrency:
    group: ci-${{ github.event_name }}-${{ github.ref }}
    cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}
  ```

- **Rationale**: The group is per event and per ref (`refs/heads/<branch>` for pushes,
  `refs/pull/<n>/merge` for pull requests), so a new push cancels only that branch's previous run of the same
  kind (FR-008) and the push and pull-request runs of the same commit do not cancel each other. On `main`
  runs are never cancelled mid-flight; GitHub keeps at most one pending run per group and supersedes older
  pending ones, so `main` runs execute one at a time in push order. `latest` is therefore always written by the
  newest finished run and never moves back (FR-011). The cost: when three commits land on `main` while a run is
  in progress, the middle one is superseded and gets no SHA tag (accepted in the spec).
- **Alternatives considered**: cancel on `main` too (an older run could still finish its publish after a newer
  one if cancellation races the push step); no concurrency control on `main` (parallel runs could finish out of
  order and move `latest` back to an older commit).

## R6. Permissions and action pinning

- **Decision**: Workflow-level `permissions: contents: read`. The `publish` job overrides it with
  `contents: read` and `packages: write`. Cache access (`type=gha` and `actions/cache`) uses the runtime token
  and needs no extra permission. Every third-party action is pinned by full commit SHA with the release tag in a
  trailing comment:

  | Action | Release | Commit SHA |
  |--------|---------|------------|
  | `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
  | `actions/cache` (`/restore`, `/save`) | v6.1.0 | `55cc8345863c7cc4c66a329aec7e433d2d1c52a9` |
  | `docker/setup-buildx-action` | v4.4.1 | `f87e5991a6d7451dcb8d9637bfbc97413f497069` |
  | `docker/build-push-action` | v7.4.0 | `c3c9e263c25d99ce0380d002d59b67737d91b0dc` |
  | `docker/login-action` | v4.7.0 | `81c6f93d4bc991e510b342a00b419b3bb4efc5c6` |
  | `docker/metadata-action` | v6.2.0 | `dc802804100637a589fabce1cb79ff13a1411302` |

  `actions/checkout` uses `persist-credentials: false`, so no job leaves the token in `.git/config` where a
  `make` target or build step could read it.
  `.github/dependabot.yml` enables weekly `github-actions` updates, which bump the SHA and the comment together.
- **Rationale**: Tags are mutable; a compromised action repository can move a tag to malicious code, while a
  SHA cannot change (FR-013). Without an updater, SHA pins rot silently; Dependabot is the boring default and
  opens normal pull requests that go through the same gates.
- **Alternatives considered**: pin to major tags (`@v7`, mutable, rejected by FR-013); Renovate (more
  configuration, another app to install); no updater (pins go stale).
- **Fork pull requests**: GitHub gives `pull_request` runs from forks a read-only token and no secrets
  regardless of the workflow's permissions. The gates and `docker build` need neither, and `publish` never runs
  for `pull_request` events, so fork runs pass or fail on their merits. `pull_request_target` is not used: it
  would run fork code with base-repository permissions.

## R7. Timeouts

- **Decision**: `timeout-minutes: 15` on `gate`, `15` on `image`, `20` on `publish`.
- **Rationale**: Well above the expected warm run (a few minutes) and the 10-minute target, far below the
  360-minute default (FR-014). A deadlocked test fails its check in 15 minutes instead of holding a runner for
  six hours. Go's own `go test` default timeout (10 minutes per package binary) usually fires first and names the
  hung test.
- **Alternatives considered**: adding `-timeout` to the Makefile targets (changes local behaviour; out of scope).

## R8. Docker Hub pulls

- **Decision**: Pull the pinned `golang` image anonymously; no Docker Hub login.
- **Rationale**: Each gate job pulls one image once; the volume is small, and it avoids storing a Docker Hub
  secret, which would widen what a compromised workflow could reach. If anonymous pull limits start failing runs,
  the guardrail is a read-only Docker Hub access token stored as a repository secret and a `docker/login-action`
  step in gate and image jobs, never in fork runs.
- **Alternatives considered**: mirror the golang image to GHCR (one more artifact to keep in sync with the
  Makefile digest); cache the image tarball with `actions/cache` (larger than the pull it saves).

## R9. Making checks required

- **Decision**: Document a repository ruleset for `main` in the README (CI section): require a pull request
  before merging, block force pushes and deletion, require status checks `make vet`, `make test`, `make race`,
  `make cover`, `docker build` from GitHub Actions, and require branches to be up to date. Include the same
  configuration as a `gh api` call for owners who prefer the command line. Also document making the GHCR package
  public if desired.
- **Rationale**: Rulesets are GitHub's current mechanism and cover everything classic branch protection does.
  Configuring them needs repository admin, which the workflow token does not and should not have, so the step
  stays manual and documented (spec US4).
- **Alternatives considered**: classic branch protection (still works; rulesets are the forward path and show
  bypass lists explicitly); automating via Terraform's GitHub provider (a state backend and an admin token for
  one repository setting).

## R10. Constitution amendment

- **Decision**: Amend `.specify/memory/constitution.md` to 1.1.0 in this pull request:
  - Scope and Constraints: the accepted-debt bullet keeps Prometheus metrics only; CI is removed.
  - Development Workflow: a pull request is mergeable only when the required CI checks (`make vet`,
    `make test`, `make race`, `make cover`, `docker build`) pass; developers still run the same targets locally.
  - Version line: 1.1.0, Last Amended 2026-11-04.
- **Rationale**: Adding CI is a scope change per the current text; a materially expanded workflow section is a
  MINOR bump under the constitution's own rules (FR-016).

## R11. Validating the workflow before pushing

- **Decision**: Lint the workflow locally with actionlint in a pinned container:
  `docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667 -color`.
  It is a quickstart step, not a sixth CI gate.
- **Rationale**: Catches expression and schema errors before the first push. The spec fixes the set of gates at
  five; a workflow lint gate can be added later as its own scope change.
