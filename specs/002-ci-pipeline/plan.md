# Implementation Plan: CI Pipeline

**Branch**: `002-ci-pipeline` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/002-ci-pipeline/spec.md`

## Summary

Add one GitHub Actions workflow, `CI`, that runs on every branch push and on pull requests to `main`. A matrix
job calls the existing `make vet`, `make test`, `make race` and `make cover` targets, so CI uses the same pinned
golang container as developers do, and reports four checks named after the commands. A `docker build` job builds
the production image without pushing it. On `main`, a `publish` job that depends on all five gates pushes the
image to GHCR tagged with the full SHA and `latest`. Go caches persist between runs through two overridable
Makefile variables that CI points at bind-mounted runner directories saved with `actions/cache`. Concurrency
cancels superseded branch runs and serializes `main` runs. Permissions are read-only except `packages: write` on
`publish`, every action is pinned by commit SHA, and Dependabot keeps the pins current. The constitution is
amended to 1.1.0 and the README gains the CI section, ruleset steps and this feature's trade-offs. See
[research.md](research.md) for every decision.

## Technical Context

**Language/Version**: GitHub Actions workflow YAML; GNU make (3.81 locally, 4.x on the runner); Go 1.27.3 inside the existing pinned container (unchanged)

**Primary Dependencies**: `actions/checkout` v7.0.1, `actions/cache` v6.1.0, `docker/setup-buildx-action` v4.4.1, `docker/build-push-action` v7.4.0, `docker/login-action` v4.7.0, `docker/metadata-action` v6.2.0, all SHA-pinned (R6)

**Storage**: GitHub Actions cache (Go module and build caches, BuildKit layers); GHCR package `ghcr.io/hermeto/tiny-s3`

**Testing**: Seeded-failure branches for each gate, cancellation and cache checks per [quickstart.md](quickstart.md); actionlint locally (R11)

**Target Platform**: GitHub-hosted `ubuntu-24.04` runners; published image `linux/amd64`

**Project Type**: CI configuration for a single Go service

**Performance Goals**: Warm run under 10 minutes for all checks (SC-003); superseded run cancelled within 1 minute (SC-004)

**Constraints**: Gates only through `make`; local Makefile behaviour unchanged; least-privilege token; no secrets beyond `GITHUB_TOKEN`; no `pull_request_target`

**Scale/Scope**: 1 workflow (3 jobs, 4 matrix legs), 1 Dependabot config, 2-line Makefile change, README and constitution updates

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Durability Before Acknowledgement | N/A (pass) | No storage code changes. |
| II. Streaming, Bounded Memory | N/A (pass) | No request-path changes. |
| III. Own Engine, Standard Library First | Pass | No Go modules added. Actions are CI tooling, not imported code. |
| IV. Test-First, 100% Coverage | Pass | `make cover` becomes a required check; CI cannot run a weaker gate because it calls the same target. `-race` enforced by the `make race` check. |
| V. Real-Client Compatibility | N/A (pass) | No S3 operation; e2e joins CI in spec 014. |
| Scope: everything in Docker, named volume | Pass | Gates still run in the pinned container. Locally the caches stay named volumes; CI overrides only the cache mount sources (R3). |
| Scope: "CI is accepted technical debt; adding it is a scope change" | **Amendment** | This feature is that scope change. The constitution is amended to 1.1.0 in this PR (R10, FR-016), so after the amendment there is no deviation. |
| Workflow: one feature = one PR on its own branch | **Action** | Branch `002-ci-pipeline` does not exist yet; current branch is `001-server-skeleton`. Create it before implementation. |
| Workflow: mergeable only when gates pass locally | Pass (amended) | Amended to require the CI checks; local targets are unchanged and still the way to reproduce. |
| Workflow: README design section | Pass | FR-017; trade-offs from R3, R4, R5, R6 and R8 go into the README. |

Post-design re-check: no violations once the amendment lands in the same PR. Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/002-ci-pipeline/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── ci-checks.md
│   └── make.md
├── checklists/requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
.github/
├── workflows/ci.yml          # NEW: CI workflow — gate (matrix), image, publish
└── dependabot.yml            # NEW: weekly github-actions updates
Makefile                      # CHANGED: GOMOD_CACHE / GOBUILD_CACHE overridable, defaults = current volumes
README.md                     # CHANGED: CI section, ruleset steps, GHCR image, "002 CI pipeline" design notes
.specify/memory/constitution.md  # CHANGED: 1.1.0 amendment (R10)
```

**Structure Decision**: One workflow file holds every job so that `publish` can depend on the gates with plain
`needs`. No new scripts: every gate's logic stays in the Makefile and `scripts/cover-gate.sh`, and the workflow
only sets up caches and calls `make`.

## Key implementation notes

- **Workflow shape** (full contract in [contracts/ci-checks.md](contracts/ci-checks.md)):
  `gate` (matrix vet/test/race/cover, `fail-fast: false`) and `image` run in parallel; `publish` has
  `needs: [gate, image]` and `if: github.event_name == 'push' && github.ref == 'refs/heads/main'`.
- **Gate job steps**: checkout (`persist-credentials: false`) → restore module cache → restore build cache →
  `make ${{ matrix.target }}` with `GOMOD_CACHE=${{ runner.temp }}/gomod` and
  `GOBUILD_CACHE=${{ runner.temp }}/gobuild` → on `main` only, save both caches (`if: always()` is not used, so a
  failed run never saves a cache).
- **Module cache key collisions**: four matrix legs on `main` try to save the same `gomod-…` key; the first wins
  and the others log "cache already exists" without failing. That is expected.
- **Makefile change**: only the two `-v` lines change, from literal volume names to `$(GOMOD_CACHE)` and
  `$(GOBUILD_CACHE)`; `clean-cache` keeps removing the named volumes. The variables are documented in the
  Makefile comment and in [contracts/make.md](contracts/make.md).
- **Root-owned files on the runner**: the container runs as root and writes into the bind-mounted checkout and
  cache directories. Runners are ephemeral, so nothing cleans them up; `actions/cache` can read them (R3).
- **Image tags**: `ghcr.io/${{ github.repository }}` lower-cased by `docker/metadata-action`; tags
  `${{ github.sha }}` and `latest`; OCI labels from the action. `provenance` left at the action default.
- **README**: new "CI" section (what runs, check names, how to reproduce, image location) and "Protecting main"
  (ruleset UI steps and `gh api` equivalent, package visibility); design notes under "002 CI pipeline".

## Complexity Tracking

No violations to justify.
