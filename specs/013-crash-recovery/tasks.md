# Tasks: Crash Recovery and Concurrency Guarantees

**Input**: Design documents from `/specs/013-crash-recovery/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required (constitution IV and V); every test task is written first and observed failing.

## Phase 1: Setup

No setup needed.

---

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T001 Write failing tests in `internal/storage/recovery_test.go` for `Store.Recovery()` on a fresh store (all zero) and after reopening a store with two buckets and three objects (`ObjectsIndexed` 3, `Buckets` 2, the rest 0)
- [ ] T002 Add `RecoveryReport` and `Recovery()` in `internal/storage/recovery.go` and fill `Buckets` and `ObjectsIndexed` in `Open`

---

## Phase 3: User Story 2 - Reclaim space left by crashes (Priority: P1) 🎯 MVP

Implemented before User Story 1 because the crash test needs the cleanup and the report.

- [ ] T003 [P] [US2] Write failing tests in `internal/storage/recovery_test.go`: leftovers in `tmp/` are removed and counted (`TmpRemoved`); bodies in `data/` that no valid record references (planted files, the body of an undecodable record) are removed and counted (`OrphansRemoved`) while referenced bodies stay; a record whose body is missing is left out of the index, counted in `RecordsSkipped` and kept on disk, and so are undecodable and misplaced records
- [ ] T004 [P] [US2] Write failing tests in `internal/storage/recovery_test.go` with `fsx.Faulty`: a failed `ReadDir` of `data/` and a failed removal of an orphan or of a temp file fail `Open` with an error naming the path
- [ ] T005 [US2] Implement the startup sequence in `internal/storage/storage.go`, `internal/storage/index.go` and `internal/storage/recovery.go` (research R2)
- [ ] T006 [P] [US2] Write a failing test in `internal/app/app_test.go`: `Main` logs one `recovery` line with `tmp_removed`, `orphans_removed`, `objects_indexed`, `records_skipped` and `buckets`, before `listening`, with the counts of a data dir prepared by the test
- [ ] T007 [US2] Log the recovery line in `internal/app/app.go` (research R3)

**Checkpoint**: startup cleans and reports.

---

## Phase 4: User Story 1 - Restart cleanly after a crash during uploads (Priority: P1)

- [ ] T008 [US1] Write `e2e/recovery_test.go` `TestKillDuringUploads` per research R4: 20 uploads of distinct 4 MiB bodies (10 acknowledged, 10 stalled after 1 MiB), poll `tmp/` until it holds 10 files, plant one orphan in `data/`, SIGKILL, restart; check the `recovery` line (10, 1, 10, 0, 1), `tmp/` empty, `aws s3api list-objects-v2` lists exactly the 10 acknowledged keys, `get-object` of each returns the uploaded body with MD5 = ETag, `data/` holds 10 files

---

## Phase 5: User Story 3 - The index matches the disk (Priority: P1)

- [ ] T009 [P] [US3] Write a randomized test in `internal/storage/recovery_test.go`: for a fixed seed set, 200 random Put/overwrite/Delete operations on 8 keys, each with a 1-in-3 chance of a fault injected at a random matching filesystem operation; then reopen and require the index to equal an independent scan of `meta/` (decoded records whose body exists, with size and MD5), `tmp/` to be empty and `data/` to hold exactly the referenced bodies
- [ ] T010 [US3] Make T009 pass (expected to need no code beyond T005; fix whatever it finds)
- [ ] T011 [US3] Extend `TestKillDuringUploads` in `e2e/recovery_test.go` to compare the CLI listing (key, size, ETag) with a scan of the `meta/` records in the data directory

---

## Phase 6: User Story 4 - Concurrent writes to one key (Priority: P2)

- [ ] T012 [US4] Add `TestConcurrentPutsOneKey` to `e2e/recovery_test.go`: 50 HTTP clients upload distinct 64 KiB bodies to one key at once; `get-object` returns exactly one of them with a matching ETag; `data/` holds one file and `tmp/` none

---

## Phase 7: Polish

- [ ] T013 Add the `### 013 crash recovery and concurrency` design section to `README.md` (research R1–R5) and document the `recovery` log line in "Running s3d"
- [ ] T014 Run `make vet test race cover e2e`; 100% for every `internal/` package
- [ ] T015 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

- T001–T002 → US2 (T003–T007) → US1 (T008) → US3 (T009–T011) → US4 (T012) → Polish.

## Parallel Opportunities

- T003/T004 (storage) with T006 (app).

## Implementation Strategy

Cleanup and reporting first (they make the crash test meaningful), then the crash test, the index-equals-disk proof and the contention test.
