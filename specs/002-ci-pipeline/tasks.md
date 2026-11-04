---

description: "Task list for the tiny-s3 CI pipeline"
---

# Tasks: CI Pipeline

**Input**: Design documents from `/specs/002-ci-pipeline/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ (ci-checks.md, make.md), quickstart.md

**Tests**: No Go code changes, so no unit tests. Each story ends with validation tasks taken from
[quickstart.md](quickstart.md); they are the acceptance tests and must be run, not skipped. Lint the workflow
with actionlint (quickstart step 3) after every edit to `.github/workflows/ci.yml`.

**Organization**: Tasks are grouped by user story. Most implementation tasks edit the same file,
`.github/workflows/ci.yml`, so they are sequential; [P] marks only tasks that touch other files.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 = five gates on every push/PR, US2 = cancellation and caches, US3 = publish on main, US4 = required checks documented
- All commands run from the repository root. Pushing to GitHub is done by the user or with the user's explicit approval; sandboxed `gh` calls may fail TLS, so use `curl` with `gh auth token` for API reads.
- Action pins (research.md R6), use exactly these, with the release tag as a trailing comment:
  `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`,
  `actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0`,
  `actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9 # v6.1.0`,
  `docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4.4.1`,
  `docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7.4.0`,
  `docker/login-action@81c6f93d4bc991e510b342a00b419b3bb4efc5c6 # v4.7.0`,
  `docker/metadata-action@dc802804100637a589fabce1cb79ff13a1411302 # v6.2.0`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Branch and the one Makefile change CI depends on.

- [X] T001 Create and switch to branch `002-ci-pipeline` from `001-server-skeleton` (`git switch -c 002-ci-pipeline`) in the repository root; the `specs/002-ci-pipeline/` files come along uncommitted
- [X] T002 In `Makefile`, add `GOMOD_CACHE ?= tiny-s3-gomod` and `GOBUILD_CACHE ?= tiny-s3-gobuild` above `DOCKER_RUN`, and change the two cache mounts to `-v "$(GOMOD_CACHE)":/go/pkg/mod` and `-v "$(GOBUILD_CACHE)":/root/.cache/go-build`. Extend the existing cache comment: a bare name is a named volume, an absolute path is a bind mount (used by CI), and the names deliberately differ from Go's `GOMODCACHE`/`GOCACHE` because make imports environment variables. Leave `clean-cache` removing the named volumes (contracts/make.md, research.md R3)
- [X] T003 Verify the Makefile change: `make vet test race cover` with no variables exits 0 and still uses the `tiny-s3-gomod`/`tiny-s3-gobuild` volumes; then `make test GOMOD_CACHE="$PWD/.tmp/gomod" GOBUILD_CACHE="$PWD/.tmp/gobuild"` exits 0 and writes into `.tmp/gobuild`; remove `.tmp` afterwards (quickstart steps 1–2). Record the commands and output in the task notes

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Workflow skeleton every job hangs off, and the constitution amendment that makes this feature in scope.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T004 Create `.github/workflows/ci.yml` with: `name: CI`; `on.push.branches: ['**']`; `on.pull_request.branches: [main]` (default types); workflow-level `permissions: contents: read`; an empty `jobs:` map ready for the following tasks. Add a top comment pointing to `specs/002-ci-pipeline/contracts/ci-checks.md` and stating that job names are the required-check names (contracts/ci-checks.md "Triggers", "Permissions"). Do not use `pull_request_target` or `workflow_run`
- [X] T005 [P] Amend `.specify/memory/constitution.md` to 1.1.0 (research.md R10): in Scope and Constraints change the accepted-debt bullet to cover Prometheus metrics only; in Development Workflow require the CI checks `make vet`, `make test`, `make race`, `make cover` and `docker build` to pass before merge, noting developers run the same targets locally; set `**Version**: 1.1.0 | **Ratified**: 2026-11-04 | **Last Amended**: 2026-11-04`. Keep everything else byte for byte

**Checkpoint**: Workflow file exists and lints clean (quickstart step 3); constitution permits CI.

---

## Phase 3: User Story 1 - Every change is validated by the same gates as local (Priority: P1) 🎯 MVP

**Goal**: Every push and every PR to `main` reports five checks (`make vet`, `make test`, `make race`, `make cover`, `docker build`), each calling the same command a developer runs.

**Independent Test**: Push the branch and open a PR; five checks per event, all green. Seeded-failure branches fail the matching checks only (quickstart steps 4–5).

### Implementation for User Story 1

- [X] T006 [US1] In `.github/workflows/ci.yml`, add job `gate`: `name: make ${{ matrix.target }}`; `runs-on: ubuntu-24.04`; `timeout-minutes: 15`; `strategy.fail-fast: false`; `strategy.matrix.target: [vet, test, race, cover]`; steps: (1) `actions/checkout` (pinned) with `persist-credentials: false`; (2) `run: make ${{ matrix.target }}`. No caches yet; the Makefile defaults to named volumes, which start empty on the runner (research.md R2, contracts/ci-checks.md "Checks")
- [X] T007 [US1] In `.github/workflows/ci.yml`, add job `image`: `name: docker build`; `runs-on: ubuntu-24.04`; `timeout-minutes: 15`; steps: `actions/checkout` (pinned, `persist-credentials: false`), `docker/setup-buildx-action` (pinned), `docker/build-push-action` (pinned) with `context: .`, `push: false`, `platforms: linux/amd64`, `cache-from: type=gha,scope=image`. No `needs` on `gate` (FR-006). Leave `cache-to` for T013
- [X] T008 [US1] Lint `.github/workflows/ci.yml` with the pinned actionlint image (quickstart step 3); fix every finding until it exits 0 with no output
- [ ] T009 [US1] Commit the Setup, Foundational and US1 changes (Conventional Commits, e.g. `build: make Go cache mounts overridable`, `docs: amend constitution to 1.1.0 for CI`, `ci: run vet, test, race, cover and image build on every push`), then ask the user to push `002-ci-pipeline` and open a draft PR to `main`. Confirm via the API that each event shows exactly the five checks named in contracts/ci-checks.md and all pass (quickstart step 4)
- [ ] T010 [US1] Run the seeded-failure matrix from quickstart step 5 on throwaway branches from `002-ci-pipeline` (copylocks vet finding, failing test, data race, untested exported function under `internal/`, broken `COPY` in `Dockerfile`): confirm each fails locally first, then that CI fails exactly the checks listed in the quickstart table and the rest pass, and that each failing log shows the `make`/build command. Delete the throwaway branches locally and on GitHub afterwards

**Checkpoint**: US1 complete — gates are automatic and agree with local results. This alone is a usable MVP.

---

## Phase 4: User Story 2 - Fast feedback without wasted runs (Priority: P2)

**Goal**: A new push cancels that branch's in-progress run; Go module and build caches are restored on every run and saved from `main`.

**Independent Test**: Two quick pushes cancel the first run within a minute; a re-run with a warm cache is faster and logs a cache hit, staying under 10 minutes (quickstart steps 4 and 6).

### Implementation for User Story 2

- [X] T011 [US2] In `.github/workflows/ci.yml`, add workflow-level `concurrency: { group: ci-${{ github.event_name }}-${{ github.ref }}, cancel-in-progress: ${{ github.ref != 'refs/heads/main' }} }` with a comment explaining that `main` runs are never cancelled so `latest` never moves back (research.md R5, FR-011)
- [X] T012 [US2] In job `gate` of `.github/workflows/ci.yml`: set step-level `env: GOMOD_CACHE: ${{ runner.temp }}/gomod` and `GOBUILD_CACHE: ${{ runner.temp }}/gobuild` on the `make` step (make reads them from the environment; the `runner` context is not available in job-level `env`); before `make`, add `actions/cache/restore` (pinned) for `${{ runner.temp }}/gomod` with key `gomod-${{ runner.os }}-${{ hashFiles('go.mod', 'go.sum') }}` and a second restore for `${{ runner.temp }}/gobuild` with key `gobuild-${{ runner.os }}-${{ matrix.target }}-${{ github.sha }}` and `restore-keys: gobuild-${{ runner.os }}-${{ matrix.target }}-`; after `make`, add `actions/cache/save` (pinned) for each path with the same primary keys, guarded by `if: github.event_name == 'push' && github.ref == 'refs/heads/main'` (no `always()`, so failed runs never save). Comment that duplicate saves of the shared `gomod-` key from parallel legs log "already exists" and are expected (research.md R3, data-model.md "Cache entry")
- [X] T013 [US2] In job `image` of `.github/workflows/ci.yml`, add `cache-to: ${{ (github.event_name == 'push' && github.ref == 'refs/heads/main') && 'type=gha,mode=max,scope=image' || '' }}` to `docker/build-push-action`, so only `main` writes the layer cache (research.md R4)
- [X] T014 [US2] Lint `.github/workflows/ci.yml` with actionlint (quickstart step 3) until clean; commit (`ci: cancel superseded runs and reuse Go caches`)
- [ ] T015 [US2] After the user pushes: run quickstart step 6 (two empty commits 15 s apart; first run `cancelled` within 1 minute, other branches untouched). Branch runs have no saved cache until `main` has one, so record the cold-run wall-clock time now and repeat the warm-run measurement in T021 after the first `main` run

**Checkpoint**: US2 complete — superseded runs are cancelled and caches are wired; warm timing is confirmed after merge.

---

## Phase 5: User Story 3 - Every commit on main has a published image (Priority: P2)

**Goal**: On push to `main`, after all five checks pass, the image is pushed to GHCR tagged with the full SHA and `latest`.

**Independent Test**: After merge, `ghcr.io/hermeto/tiny-s3:<sha>` and `:latest` resolve to the same digest and the container answers with the `NotImplemented` XML error (quickstart step 7).

### Implementation for User Story 3

- [X] T016 [US3] In `.github/workflows/ci.yml`, add job `publish`: `name: publish`; `needs: [gate, image]`; `if: github.event_name == 'push' && github.ref == 'refs/heads/main'`; `runs-on: ubuntu-24.04`; `timeout-minutes: 20`; job-level `permissions: { contents: read, packages: write }` (the only job with any write permission, FR-012). Steps: `actions/checkout` (pinned, `persist-credentials: false`); `docker/setup-buildx-action` (pinned); `docker/login-action` (pinned) with `registry: ghcr.io`, `username: ${{ github.actor }}`, `password: ${{ secrets.GITHUB_TOKEN }}`; `docker/metadata-action` (pinned, `id: meta`) with `images: ghcr.io/${{ github.repository }}` and `tags: |` lines `type=raw,value=${{ github.sha }}` and `type=raw,value=latest`; `docker/build-push-action` (pinned) with `context: .`, `platforms: linux/amd64`, `push: true`, `tags: ${{ steps.meta.outputs.tags }}`, `labels: ${{ steps.meta.outputs.labels }}`, `cache-from: type=gha,scope=image` (research.md R4, contracts/ci-checks.md "Published image")
- [ ] T017 [US3] Lint `.github/workflows/ci.yml` with actionlint (quickstart step 3) until clean; commit (`ci: publish the image to GHCR on main`). After the user pushes, confirm in the PR runs that `publish` is skipped and that no package version appears under the repository's packages (US3 scenario 3)

**Checkpoint**: US3 implemented; its runtime proof happens after merge (T021).

---

## Phase 6: User Story 4 - Repository owner can make the checks mandatory (Priority: P3)

**Goal**: Written, accurate steps to require the five checks on `main`.

**Independent Test**: Follow the steps; a PR with a failing required check cannot merge and a direct push to `main` is rejected (quickstart step 8).

### Implementation for User Story 4

- [X] T018 [P] [US4] In `README.md`, add a "Protecting main" section (research.md R9): ruleset UI steps (Settings → Rules → Rulesets → New branch ruleset, target `main`, enforcement Active; enable "Restrict deletions", "Block force pushes", "Require a pull request before merging", "Require status checks to pass" with "Require branches to be up to date before merging", adding the checks `make vet`, `make test`, `make race`, `make cover`, `docker build` from source GitHub Actions); the equivalent `gh api --method POST repos/hermeto/tiny-s3/rulesets` call with a JSON body (rules `deletion`, `non_fast_forward`, `pull_request`, `required_status_checks` with `strict_required_status_checks_policy: true` and the five contexts); a note that renaming a job requires updating the ruleset in the same PR; and how to make the `tiny-s3` GHCR package public (package settings → Danger Zone → Change visibility)

**Checkpoint**: US4 documented; enforcement verified after merge (T021).

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Pin maintenance, README, and post-merge verification of US2–US4.

- [X] T019 [P] Create `.github/dependabot.yml`: `version: 2`, one update entry with `package-ecosystem: github-actions`, `directory: /`, `schedule.interval: weekly`, and a `commit-message.prefix: ci` so its PRs follow Conventional Commits (research.md R6)
- [X] T020 [P] In `README.md`, add a "CI" section (what runs on which events, the five check names and that each is reproduced by the same `make` command or `docker build .`, where the image is published and its tags, the `GOMOD_CACHE`/`GOBUILD_CACHE` overrides, and a note that local runs on Apple Silicon use the arm64 image variant, so an architecture-dependent failure may appear only in CI; the CI log is the source of truth, and reproducing under amd64 emulation is best-effort since `-race` under emulation may not work) and a "### 002 CI pipeline" entry under "## Design" recording: gates call `make` instead of `setup-go`; caches via overridable bind mounts and why the names avoid `GOMODCACHE`/`GOCACHE`; caches saved only from `main`; push plus PR triggers run each PR commit twice (accepted); `main` runs serialized, never cancelled, so an intermediate commit may lack a SHA tag; publish rebuilds from the layer cache rather than shipping a tarball artifact (revisit for spec 014); SHA pins plus Dependabot; anonymous Docker Hub pulls with the token fallback; `persist-credentials: false` (FR-017)
- [ ] T021 Run the full quickstart: steps 1–3 locally; mark the PR ready; after the user merges to `main`, run step 7 (pull both tags, compare digests, run the container, `curl -s localhost:9000/`); record the PR's `merged_at` and the `publish` job's `completed_at` (`curl -s -H "Authorization: Bearer $(gh auth token)" https://api.github.com/repos/hermeto/tiny-s3/pulls/<n>` and `.../actions/runs/<run-id>/jobs`) and confirm the difference is under 15 minutes (SC-005); re-run all jobs of that successful `main` run and confirm `publish` passes and `ghcr.io/hermeto/tiny-s3:<sha>` and `:latest` still resolve to the same digest (the digest may differ from the first run's because provenance metadata changes; the layers must not, compare with `docker buildx imagetools inspect --raw`); then re-run a branch push to confirm the warm-cache run is faster than T015's cold run and under 10 minutes (SC-003), then step 8 after the owner applies the ruleset. Record each command and its output; report any failing step instead of marking this task done

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none. T002 before T003.
- **Foundational (Phase 2)**: after Setup. T004 blocks every workflow task; T005 is independent.
- **US1 (Phase 3)**: after Foundational. Delivers the MVP.
- **US2 (Phase 4)**: after US1 (edits the `gate` and `image` jobs T006/T007 created). Needs T002 for the cache overrides.
- **US3 (Phase 5)**: after US1 (needs `gate` and `image` to exist for `needs`). Independent of US2 logically, but sequential with it because both edit `ci.yml`.
- **US4 (Phase 6)**: depends only on the final check names (T006, T007); can be written any time after them.
- **Polish (Phase 7)**: T019 and T020 any time after T004; T021 last, after merge.

### Parallel Opportunities

- T005 (constitution) alongside T004 (workflow skeleton).
- T018 (README ruleset section), T019 (Dependabot) and T020 (README CI section) alongside any `ci.yml` task; T018 and T020 edit different sections of `README.md`, so do them in one sitting or rebase carefully.
- In CI itself: the four `gate` legs and `image` run in parallel by design.

### Parallel Example: after T007

```text
Task: "T018 [P] [US4] Protecting main section in README.md"
Task: "T019 [P] Create .github/dependabot.yml"
while sequentially: T011 → T012 → T013 → T014 in .github/workflows/ci.yml
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (T001–T003) and Phase 2 (T004–T005).
2. Phase 3 (T006–T010): gates run on every push and PR and agree with local results.
3. Stop and validate: this already turns the local merge gate into an automatic one.

### Incremental Delivery

1. US1 → gates automatic (MVP).
2. US2 → cancellation and caches (speed).
3. US3 → image published from `main`.
4. US4 → checks made mandatory.

All of it ships in one PR (constitution: one feature, one PR); the phases are commit boundaries inside that PR.
