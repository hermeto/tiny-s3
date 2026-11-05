# Tasks: End-to-End Suite and Demo Script

**Input**: Design documents from `/specs/014-e2e-suite-demo/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: This feature is test tooling. Each check is written against `contracts/suite.md` before the orchestration that runs it, and the suite itself is checked for the failures it must catch (T006, T007).

## Phase 1: Setup

- [ ] T001 Add `diffutils-3.8-1.amzn2023.0.2` to `e2e/Dockerfile` (dnf, no weak deps, cache cleaned) and check `cmp --version` runs in the built image
- [ ] T002 [P] Add a `shellcheck` target to `Makefile` running `koalaman/shellcheck:v0.10.0@sha256:2097951f02e735b613f4a34de20c40f937a6c8f18ecb170612c88c34517221fb` over `scripts/`

---

## Phase 2: Foundational

- [X] T003 Write `scripts/e2e/lib.sh`: run-scoped names (`tiny-s3-e2e-<pid>-…`), create network and volume, start the server (alias `s3d`, no published port) and wait for its N-th `listening` line, start the client (e2e image, `sleep infinity`, `/suite` read-only, credentials and region) and the inspector (volume read-only), `in_client`/`in_inspector` exec helpers, `pass`/`fail` output, and a cleanup function for an `EXIT INT TERM` trap; host-side code must run on Bash 3.2 (macOS default: no associative arrays, `mapfile`, `wait -n` or `${v,,}`)

---

## Phase 3: User Story 1 - One command proves every operation (Priority: P1) 🎯 MVP

- [X] T004 [US1] Write `scripts/e2e/checks.sh` with the groups `bucket`, `object`, `range`, `list` and `delete` per `contracts/suite.md` (keys `small.txt`, `big.bin` 100 MiB, `dir/sub/ünïcødé ✓.txt`; `cmp` for every comparison; 2,500 keys uploaded with `aws s3 cp --recursive` and paged by hand with `--no-paginate --max-keys 500`)
- [X] T005 [US1] Write `scripts/e2e/run.sh`: build the server image from `Dockerfile` and the e2e image, start the run's resources, run the groups in order, print `e2e suite: PASSED` or the failed check with the server's log tail, exit 0/1
- [X] T006 [US1] Check the suite catches breakage (SC-002): run it against an image with one operation broken on purpose (a temporary local change that is not committed, e.g. a wrong ETag) and confirm it exits 1 naming the failed check; revert and confirm `git status` shows no source change; record the run in the implementation notes
- [X] T007 [US1] Check cleanup (SC-003): after a passing run, a failing run and a run interrupted with SIGINT, no container, network or volume named `tiny-s3-e2e-` remains; and a running Compose stack still answers on port 9000 during and after a run

---

## Phase 4: User Story 2 - Crash test against the container (Priority: P1)

- [X] T008 [US2] Add the `crash-upload` and `crash-verify` groups to `scripts/e2e/checks.sh` and the crash orchestration to `scripts/e2e/run.sh` per research R4 (20 × 32 MiB uploads, kill once `tmp/` holds a file, `docker start`, recovery line from the restart, empty `tmp/`, acknowledged keys listed, every listed key equal to its source with MD5 = ETag, one body per listed object)

---

## Phase 5: User Story 3 - The checks run in CI (Priority: P1)

- [X] T009 [US3] Make `make e2e` run the Go e2e tests, `scripts/e2e/run.sh` and `DEMO_PAUSE=0 scripts/demo.sh` in `Makefile` (GNU make 3.81 compatible)
- [X] T010 [US3] Add the `make e2e` job to `.github/workflows/ci.yml` (ubuntu-24.04, 30-minute timeout, its own Go build cache restored on every run and saved from main, same triggers) and add it to `publish`'s `needs`

---

## Phase 6: User Story 4 - A narrated demo (Priority: P2)

- [X] T011 [US4] Write `scripts/demo.sh` on top of `scripts/e2e/lib.sh`: ten narrated steps (start, bucket, upload, download and compare, range, folders, crash and recovery, delete, list, cleanup) that echo each command, pause with `read` unless `DEMO_PAUSE=0`, and exit 1 on any failure

---

## Phase 7: Polish

- [X] T012 Update `README.md`: Development table (`make e2e`, `make shellcheck`), CI table and ruleset JSON with `make e2e`, the demo, and a `### 014 end-to-end suite and demo` design section
- [X] T013 Run `make vet test race cover e2e` and `make shellcheck`; 100% for every `internal/` package
- [X] T014 Run `quickstart.md`, including the Compose check, then `docker compose down -v`; record the demo's duration without pauses (SC-004: under 3 minutes)
- [ ] T015 After pushing, confirm the `make e2e` check runs and passes in CI on the pull request's branch, and record its duration (SC-001: under 15 minutes)

## Dependencies & Execution Order

- T001 → T003 → US1 (T004–T007) → US2 (T008) → US3 (T009–T010) → US4 (T011) → Polish. T009 needs T011's script to exist; write T011 before running T009's target end to end.

## Parallel Opportunities

- T002 with T001. The rest edits the same few scripts in sequence.

## Implementation Strategy

Library and checks first, then the crash test, then wiring into make and CI, then the demo.

## Analysis Remediation (2026-11-05)

`/speckit-analyze` findings, fixed with the recommended option:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| C1 | MEDIUM | FR-008 (Docker and Bash only) holds only if the host-side scripts run on macOS's Bash 3.2, which the plan states but no task enforced. | T003 names the Bash 3.2 constraints for all host-side code. |
| U1 | MEDIUM | SC-001's 15-minute CI bound had no task measuring it. | T015 records the CI duration. |
| U2 | LOW | SC-004's 3-minute demo bound had no task measuring it. | T014 records the demo duration. |
| U3 | LOW | T006's deliberate breakage could leak into a commit. | T006 checks `git status` after reverting. |

No constitution conflicts: the new CI check is named after its command and runs the Makefile target; the required checks are unchanged. Coverage: FR-001–FR-010 and SC-001–SC-004 each map to at least one task (100%).

## Implementation Notes (2026-11-05)

- T006: with the PutObject ETag broken on purpose (`"<md5>0"`, never committed), `scripts/e2e/run.sh` exited 1 with `FAIL object/put-small: ETag "…0", want "…"`; after the revert `git status` showed no change under `internal/`.
- T007: after a passing and a failing run nothing named `tiny-s3-e2e-` remained; SIGINT and SIGTERM mid-run exited 130 and 143 (TERM changed from 130 during the task) and removed everything; a Compose stack answered 200 on port 9000 during and after a run. A first SIGINT attempt was invalid (a background job in a non-interactive shell ignores SIGINT), so the signals were sent from a Python parent.
- T008: the first crash design killed the server with nothing acknowledged or nothing in flight, depending on timing. The final design throttles ten `aws s3 cp` uploads to 4 MB/s (no retries) and kills only after the ten `put-object` uploads are acknowledged and five throttled ones hold temp files; the restart must report removed temp files. Two consecutive runs: 10 acknowledged, 10 in flight, `tmp_removed` 10, 2,517 objects and bodies.
- Measured: `make e2e` 245 s locally; the demo without pauses 7 s (SC-004: under 3 minutes).
