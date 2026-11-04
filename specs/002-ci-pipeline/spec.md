# Feature Specification: CI Pipeline

**Feature Branch**: `002-ci-pipeline`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Continuous integration for tiny-s3 with GitHub Actions, so every push and pull request is validated automatically and nothing reaches main without passing the same gates a developer runs locally. Acceptance criteria: A workflow runs on every push to any branch and on every pull request targeting main. It runs `make vet`, `make test`, `make race` and `make cover` through the same Docker-based Makefile targets used locally, so CI and local results never disagree; the coverage gate fails the run when any package under internal/ is below 100%. It builds the Docker image, proving the Dockerfile still works. Go module and build caches are reused between runs; a typical run finishes in under 10 minutes. A new push to a branch cancels that branch's run still in progress. Each gate shows as a separate, clearly named check on the pull request, so a failure points straight at what broke. On a push to main, the image is published to GitHub Container Registry, tagged with the commit SHA and `latest`. The workflow uses least-privilege permissions (read-only by default; package write only for the publish job) and pins third-party actions to a version. The steps to make the checks required on main (branch protection) are documented for the repository owner. Out of scope: deploying the server anywhere. End-to-end tests are added to this workflow by spec 014."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Every change is validated automatically by the same gates as local (Priority: P1)

A developer pushes a commit to any branch, or opens or updates a pull request against `main`. Without doing anything else, the repository runs vet, unit tests, race tests and the coverage gate using the exact `make` targets the developer runs on their laptop, and builds the container image. Each of the five gates appears as its own named check, so when one fails the developer sees which one from the pull request page and can reproduce it locally with the matching `make` command.

**Why this priority**: This is the core of the feature. Today the merge gate depends on each developer remembering to run the targets locally; automating it is what makes the gate real.

**Independent Test**: Push a branch with a deliberate failure in each gate in turn (a vet finding, a failing test, a data race, an uncovered statement under `internal/`, a broken Dockerfile) and confirm that the matching check fails and the checks that do not exercise the seeded defect pass (a failing test also fails race and cover, which run the same tests); then push a clean commit and confirm all five pass.

**Acceptance Scenarios**:

1. **Given** a commit pushed to any branch, **When** the push lands, **Then** a run starts automatically and reports five checks: vet, test, race, cover and image build.
2. **Given** a pull request targeting `main`, **When** it is opened, reopened or receives a new commit, **Then** the same five checks run against the pull request and their status appears on it.
3. **Given** a commit where `make cover` reports a package under `internal/` below 100% statement coverage, **When** CI runs, **Then** the cover check fails, the others are unaffected, and the check log names the package and its coverage as the local command does.
4. **Given** any gate passes or fails locally for a commit, **When** CI runs that commit, **Then** CI reports the same result, because it invokes the same `make` target with the same pinned toolchain (except for architecture-dependent behaviour; see Assumptions).
5. **Given** a Dockerfile that no longer builds, **When** CI runs, **Then** the image build check fails and nothing is published.
6. **Given** a check fails, **When** the developer opens its log, **Then** the log shows the `make` command that ran, so it can be reproduced locally verbatim.

---

### User Story 2 - Fast feedback without wasted runs (Priority: P2)

A developer pushes several commits in quick succession while iterating on a pull request. Only the latest commit's run keeps going; earlier in-progress runs on the same branch are cancelled. Repeat runs reuse downloaded modules and compiled packages, so feedback arrives in minutes.

**Why this priority**: Slow or noisy CI gets ignored or bypassed. It matters, but the gates are valuable even before they are fast.

**Independent Test**: Push two commits to the same branch a few seconds apart and confirm the first run is cancelled; then rerun an unchanged commit and confirm the module/build cache was restored and the run time drops compared with a cold run.

**Acceptance Scenarios**:

1. **Given** a run in progress for branch X, **When** a new commit is pushed to branch X, **Then** the in-progress run for X is cancelled and a new run starts; runs on other branches are untouched.
2. **Given** a previous run populated the module and build caches, **When** a later run for the same or a related branch starts with unchanged dependencies, **Then** it restores those caches instead of downloading modules and rebuilding from scratch.
3. **Given** `go.mod`/`go.sum` changed, **When** CI runs, **Then** it does not reuse a stale module cache in a way that hides a missing or wrong dependency (`-mod=readonly` still applies).
4. **Given** a typical commit (code change, no dependency change, warm cache), **When** CI runs, **Then** all checks complete in under 10 minutes wall-clock.

---

### User Story 3 - Every commit on main has a published image (Priority: P2)

When a commit lands on `main` and all gates pass, the container image is published to GitHub Container Registry under the repository's package, tagged with the full commit SHA and with `latest`. Anyone can pull the image for a given commit without building it.

**Why this priority**: Gives a reproducible artifact per main commit, which later work (e2e in spec 014, any future deployment) builds on. Validation (US1) must exist first.

**Independent Test**: Merge a pull request to `main`, then pull `ghcr.io/<owner>/tiny-s3:<full-sha>` and `:latest` and confirm both resolve to the same digest and the container starts.

**Acceptance Scenarios**:

1. **Given** a push to `main` where all five gates pass, **When** the run finishes, **Then** the image is published with two tags: the full commit SHA and `latest`, both pointing to the same digest.
2. **Given** a push to `main` where any gate fails, **When** the run finishes, **Then** nothing is published and `latest` keeps pointing to the previous good commit.
3. **Given** a push to any branch other than `main`, or any pull request, **When** CI runs, **Then** the image is built but never published.
4. **Given** two commits land on `main` close together, **When** both runs complete, **Then** `latest` ends up pointing to the newer commit, never moved back to the older one.

---

### User Story 4 - Repository owner can make the checks mandatory (Priority: P3)

The repository owner follows written steps to configure branch protection on `main` so that a pull request cannot merge unless the five checks pass, and direct pushes to `main` are blocked.

**Why this priority**: Without it, the checks are advisory. It is a one-time manual setup in repository settings, so it is documented rather than automated.

**Independent Test**: Follow the documented steps on the repository, open a pull request with a failing check, and confirm the merge button is blocked until the check passes.

**Acceptance Scenarios**:

1. **Given** the documentation, **When** the owner follows it, **Then** the five check names to mark as required are listed exactly as they appear on pull requests.
2. **Given** branch protection is configured, **When** a pull request has a failing or missing required check, **Then** it cannot be merged.

---

### Edge Cases

- A pull request from a fork: gates run with a read-only token and no access to secrets; no publish is attempted, and the run must not fail because of missing write permissions.
- A branch push that is also an open pull request: both the push run and the pull request run happen; each is cancelled independently by newer commits to the same branch. Accepted duplication (see Assumptions).
- Cache unavailable, evicted or corrupt: the run proceeds with a cold cache and is slower, but its result is unchanged.
- Cache produced by an untrusted branch: a pull request run must not be able to write a cache entry that `main` runs later restore.
- Registry outage or rate limit during publish: the publish step fails visibly and the run is marked failed; a re-run of the same commit publishes the same SHA tag without error.
- Re-running a successful `main` run: publishing succeeds again; the SHA tag and `latest` move together and may get a new digest (provenance metadata changes), with unchanged image layers.
- A gate hangs (e.g. a deadlocked test): it is stopped by a timeout well before the platform default, and the check fails with a clear timeout message.
- Several commits land on `main` while a run is in progress: the running `main` run completes; of the runs queued behind it only the newest is kept and older queued runs are superseded. `latest` ends on the newest commit; a superseded intermediate commit gets no SHA tag (see Assumptions).
- A workflow change itself breaks CI: the broken workflow fails on its own branch before it can reach `main`, once branch protection is in place.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: CI MUST start automatically on every push to any branch and on every pull request event (opened, reopened, synchronized) targeting `main`.
- **FR-002**: CI MUST run vet, unit tests, race tests and the coverage gate by invoking the existing `make vet`, `make test`, `make race` and `make cover` targets, with no CI-only reimplementation of their commands.
- **FR-003**: The coverage check MUST fail when any package under `internal/` is below 100% statement coverage or has no tests, exactly as `make cover` does locally.
- **FR-004**: CI MUST build the production container image from the repository's Dockerfile on every run.
- **FR-005**: Each of the five gates (vet, test, race, cover, image build) MUST be reported as a separate check with a stable, human-readable name that identifies the gate; the names MUST NOT change between runs, so they can be marked required.
- **FR-006**: The gates MUST be independent: a failure in one MUST NOT prevent the others from running and reporting their own result.
- **FR-007**: Go module and build caches MUST be persisted between runs and restored when available; a missing cache MUST NOT change any gate's result.
- **FR-008**: A new push to a branch MUST cancel any in-progress run for that same branch and event; runs for other branches MUST NOT be affected.
- **FR-009**: On a push to `main`, and only when all five gates pass, CI MUST publish the image to GitHub Container Registry tagged with the full commit SHA and `latest`.
- **FR-010**: CI MUST NOT publish images from branches other than `main` or from pull requests.
- **FR-011**: `latest` MUST never be moved to a commit older than the one it currently points to.
- **FR-012**: The default token permission for every job MUST be read-only repository contents; only the publish job MAY be granted package write, and no job MAY receive any other write permission.
- **FR-013**: Every third-party action MUST be pinned to an immutable reference (full commit SHA), with the corresponding release version recorded next to it for readability.
- **FR-014**: Every job MUST have an explicit timeout shorter than the platform default.
- **FR-015**: The repository MUST document, for the owner, the steps to protect `main`: require pull requests, require the five named checks to pass, and require branches to be up to date before merging.
- **FR-016**: The constitution's Scope and Constraints and Development Workflow sections MUST be amended in the same pull request to remove CI from the accepted technical debt and to make the CI checks part of the merge gate, with a MINOR version bump.
- **FR-017**: The README design section MUST record the trade-offs made by this feature (caching approach, push plus pull request duplication, cancellation policy on `main`), per the constitution's Development Workflow.

### Key Entities

- **Workflow run**: One execution of CI for a commit and event (push or pull request); belongs to a branch-scoped concurrency group.
- **Check**: The reported pass/fail result of one gate within a run, identified by a stable name.
- **Cache entry**: Saved Go module and build cache contents, keyed so that changed dependencies invalidate it and scoped so that untrusted branches cannot overwrite `main`'s entries.
- **Published image**: The container image in GitHub Container Registry for a `main` commit, addressed by its full-SHA tag and, for the newest commit, `latest`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of pushes and pull requests targeting `main` produce a run with all five checks reported, with no manual trigger.
- **SC-002**: For every gate, a commit that fails it locally fails it in CI and a commit that passes it locally passes it in CI (0 disagreements across the five seeded-failure tests in US1; architecture-dependent behaviour is excepted, see Assumptions).
- **SC-003**: A typical run with a warm cache completes all checks in under 10 minutes; a warm run is measurably faster than a cold run of the same commit.
- **SC-004**: When a second commit is pushed to a branch, the earlier in-progress run is cancelled within 1 minute.
- **SC-005**: Every commit merged to `main` with passing gates has an image pullable by its full SHA within 15 minutes of the merge, and `latest` resolves to the newest such commit.
- **SC-006**: A developer can name the broken gate from the pull request page alone, without opening any log.
- **SC-007**: With branch protection configured from the documentation, 0 pull requests with a failing required check can be merged.

## Assumptions

- The repository is hosted on GitHub (`hermeto/tiny-s3`) and uses GitHub-hosted Linux runners, which provide Docker; no self-hosted runners.
- The Makefile's Docker-based targets work unchanged in CI. Running them without a TTY is already handled. The named Docker volumes the Makefile uses for caches start empty on each runner, so persisting them between runs is CI's job; any change to the Makefile to support that MUST keep local behaviour identical.
- On Apple Silicon, local runs use the linux/arm64 variant of the pinned golang image; CI uses linux/amd64. Results can differ only for architecture-dependent behaviour, such as timing- or race-sensitive tests. When they differ, the CI result is authoritative and the test is treated as a bug to fix, not a CI flake.
- Running on both push and pull request means a pull request branch is validated twice per commit. This is accepted as the user specified both triggers; it doubles runner minutes, which are free for public repositories.
- On `main`, an in-progress run is not cancelled mid-publish; if runs are cancelled or superseded, an intermediate `main` commit may end up without a SHA tag, but `latest` always reflects the newest commit. The exact concurrency policy for `main` is a planning decision bounded by FR-011.
- The published package is public only if the repository owner makes it public; visibility is a manual setting, documented alongside branch protection.
- Images are published for a single platform (linux/amd64, the runner's native platform); multi-arch images are out of scope.
- The `make e2e` target exists but is not run in CI until spec 014.
- Out of scope: deploying the server, release tagging/semver images, image signing and SBOM generation, Prometheus metrics.
- Dependency: the constitution currently lists CI as accepted technical debt ("Adding either is a scope change"); this feature is that scope change and requires the amendment in FR-016.
