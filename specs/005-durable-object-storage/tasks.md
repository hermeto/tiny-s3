# Tasks: Durable Object Storage

**Input**: Design documents from `/specs/005-durable-object-storage/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required by the specification and constitution Principle IV; every test task is TDD-first and must be observed failing before its implementation task.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the package skeleton so tests compile against the contract.

- [ ] T001 Create `internal/storage/storage.go` with the package doc comment, the exported errors `ErrNoSuchKey`, `ErrIncompleteBody`, `ErrBadDigest`, `ErrEntityTooLarge`, `ErrKeyTooLong`, `ErrInvalidArgument`, `ErrCorruptRecord`, the constants `MaxObjectSize = 5 << 30` and `MaxKeyLength = 1024`, and the `Info` and `Reader` types from `specs/005-durable-object-storage/contracts/storage-api.md`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Read-side filesystem operations, layout, validation and locks that every story needs.

- [ ] T002 [P] Write failing tests for `OS.Open` (read, `ReadAt`, `Seek`, `fs.ErrNotExist` passthrough) and `OS.ReadDir` (entries, missing-directory error) in `internal/fsx/fsx_test.go`
- [ ] T003 [P] Write failing tests for `Faulty.Open`, `Faulty.ReadDir` and the wrapped read file (`OpOpen`, `OpRead` on `Read` and `ReadAt`, `OpReadDir`, `OpClose` on a read file, `Seek` passthrough, history records and Nth matching) in `internal/fsx/faulty_test.go`
- [ ] T004 Add `ReadFile`, `FS.Open`, `FS.ReadDir` and the `OS` implementations to `internal/fsx/fsx.go`, and `OpOpen`, `OpRead`, `OpReadDir` plus the `faultyReadFile` wrapper to `internal/fsx/faulty.go`, per `specs/005-durable-object-storage/contracts/fsx-read-ops.md`
- [ ] T005 [P] Write failing tests in `internal/storage/storage_test.go` for `Open` (creates `tmp/`, `data/`, `meta/`; removes every leftover in `tmp/`; each `MkdirAll`, `ReadDir` and `Remove` failure is returned) and for key/size validation: "Non-empty, valid UTF-8, ≤ 1024 bytes" (over 1024 → `ErrKeyTooLong`, empty or invalid UTF-8 → `ErrInvalidArgument`), "0 ≤ Size ≤ 5 GiB" (negative → `ErrInvalidArgument`, above → `ErrEntityTooLarge`)
- [ ] T006 Implement `Open`, the layout helpers (record path `meta/<hex sha256(bucket "/" key)>.json`, data path `data/<version>` with `crypto/rand.Text()`), `validateKey` and `validateSize` in `internal/storage/storage.go`, and the 64-stripe key lock in `internal/storage/lock.go`

**Checkpoint**: `make test` passes for `internal/fsx` and the storage skeleton.

---

## Phase 3: User Story 1 - Store and read back an object durably (Priority: P1) 🎯 MVP

**Goal**: Put streams a body to disk and Head/Get return it with correct size, MD5 and last-modified.

**Independent Test**: Put known bodies (including zero-length and a key with `/`, `..`, spaces and non-ASCII), then Head and Get them and compare metadata and bytes; a 64 MiB Put stays within a fixed allocation bound.

### Tests for User Story 1

- [ ] T007 [P] [US1] Write failing round-trip tests in `internal/storage/put_test.go`: Put returns size, lowercase hex MD5 and the injected clock time in UTC; Head and Get return the same `Info`; Get's reader yields the exact bytes; zero-length body; unusual keys stay inside `meta/`; same key in two buckets are distinct objects; Head/Get of a missing key → `ErrNoSuchKey`
- [ ] T008 [P] [US1] Write a failing bounded-memory test in `internal/storage/memory_test.go` that Puts 64 MiB from a generating reader and asserts `runtime.MemStats.TotalAlloc` grows by less than 4 MiB
- [ ] T009 [P] [US1] Write failing record-decoding tests in `internal/storage/read_test.go`: invalid JSON, `v != 1`, record over 64 KiB → `ErrCorruptRecord`; record open/read failures (non-`ErrNotExist`) are returned wrapped; data open failure in Get is returned and the record lock is released

### Implementation for User Story 1

- [ ] T010 [US1] Implement the Put happy path in `internal/storage/put.go` per research R2 and R5: temp file in `tmp/`, `io.CopyBuffer` with a 32 KiB buffer into `io.MultiWriter(file, md5)`, file sync, close, rename to `data/<version>`, `data/` sync, record temp write/sync/close, locked commit (read previous record, rename into `meta/`, `meta/` sync, best-effort removal of the previous data file)
- [ ] T011 [US1] Implement `Head`, `Get` (record read plus data open under the stripe lock) and record encoding/decoding per research R10 in `internal/storage/read.go`

**Checkpoint**: US1 tests pass; the MVP stores and serves objects.

---

## Phase 4: User Story 2 - Reject bad input without leaving a trace (Priority: P1)

**Goal**: Length and digest mismatches fail with the S3 error and leave no files.

**Independent Test**: Short, long and wrong-MD5 bodies fail with `ErrIncompleteBody` / `ErrBadDigest`, `tmp/` and `data/` stay empty, and an existing object under the same key is unchanged.

- [ ] T012 [P] [US2] Write failing tests in `internal/storage/put_test.go` for a short body, a long body (one extra byte), a wrong Content-MD5, a matching Content-MD5, a body reader error mid-stream, and a cancelled context; each failure leaves `tmp/` and `data/` free of new files and a previous version intact
- [ ] T013 [US2] Implement the length probe, digest comparison, context-checking reader and cleanup of the temp file on every input failure in `internal/storage/put.go`

---

## Phase 5: User Story 3 - Survive I/O failures and crashes at any write step (Priority: P1)

**Goal**: An injected failure at any step leaves no new visible version and no temp file; the success history shows the constitution's order.

**Independent Test**: The generic fault test replays a successful Put's history and fails each operation in turn, for a fresh key and for an overwrite.

- [ ] T014 [US3] Write a failing ordering test in `internal/storage/fault_test.go` asserting the successful Put history: create, writes, sync, close, rename into `data/`, opendir/dirsync/close of `data/`, create, write, sync, close of the record temp, open of the previous record, rename into `meta/`, opendir/dirsync/close of `meta/`
- [ ] T015 [US3] Write a failing generic test in `internal/storage/fault_test.go` that records a successful Put's history and, for every record k, injects a failure on the Nth occurrence of that operation in a fresh store; for a fresh key and for an overwrite it asserts Put fails (except the post-commit removal of the previous data file, which must succeed), the previous state is visible unchanged, and `tmp/` is empty
- [ ] T016 [US3] Write failing tests in `internal/storage/fault_test.go` for: cleanup `Remove` failing too (original error returned), short write without error (`io.ErrShortWrite`), `meta/` sync failure rollback with and without a previous version, and a rollback that also fails (original error joined, no partial object)
- [ ] T017 [US3] Implement error wrapping, cleanup of temp and new data files on every branch, and the rollback of research R3 in `internal/storage/put.go`

---

## Phase 6: User Story 4 - Consistent reads under concurrent overwrite and delete (Priority: P2)

**Goal**: Open readers keep their version; concurrent Puts end with one complete winner.

**Independent Test**: A reader opened before overwrite and before delete yields the original bytes; 16 concurrent Puts with distinct bodies leave one record whose metadata and bytes match one body, exactly one data file and an empty `tmp/`, under `-race`.

- [ ] T018 [P] [US4] Write failing tests in `internal/storage/concurrency_test.go` for a reader surviving overwrite, a reader surviving delete, 16 concurrent Puts plus concurrent Gets to one key, and concurrent Puts interleaved with Deletes on one key ending in either absence or exactly one complete version with no leftover data files
- [ ] T019 [US4] Fix any race found by T018 in `internal/storage/put.go`, `internal/storage/read.go` or `internal/storage/lock.go`, keeping the critical section to the commit steps of research R4

---

## Phase 7: User Story 5 - Idempotent delete (Priority: P2)

**Goal**: Delete always succeeds for missing keys and is durable for present keys.

**Independent Test**: Delete a stored key twice and a missing key once; injected failures on record removal leave the object readable.

- [ ] T020 [P] [US5] Write failing tests in `internal/storage/delete_test.go`: delete present key then Head → `ErrNoSuchKey`; delete twice; delete missing key; record read failure, record `Remove` failure (object stays readable), `meta/` open/sync failure (error returned, data file kept), data file `Remove` failure (Delete still succeeds); invalid key → validation error
- [ ] T021 [US5] Implement `Delete` in `internal/storage/delete.go`: validate key, lock stripe, read record (missing → nil), remove record, sync `meta/`, remove data file best effort

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T022 Run `make vet test race cover e2e`; fix until every package under `internal/` reports 100.0% and all targets pass
- [ ] T023 [P] Add the "005 durable object storage" trade-offs (layout, commit point, rollback, striped locks, flat directories, crash orphans, not wired yet) to the README design section in `README.md`
- [ ] T024 Run `specs/005-durable-object-storage/quickstart.md` against `docker compose up -d --build`, including the AWS CLI command, then `docker compose down -v`

## Dependencies

- T001 → T002–T006 → US1 (T007–T011) → US2, US3, US4, US5 → Polish.
- US2, US3 and US5 depend on US1's Put/Get; US4 depends on US1 and US5's Delete for the reader-survives-delete test.
- Tests in one file run sequentially; `[P]` marks tasks touching different files.

## Parallel Examples

- Foundational: T002 (`fsx_test.go`), T003 (`faulty_test.go`) and T005 (`storage_test.go`) together.
- US1: T007 (`put_test.go`), T008 (`memory_test.go`) and T009 (`read_test.go`).
- After US1: T018 (`concurrency_test.go`) and T020 (`delete_test.go`) in parallel.

## Implementation Strategy

MVP is US1 (Put/Get/Head on the happy path). US2 and US3 complete the durability guarantee and are required before the PR; US4 and US5 finish the spec. One PR for the whole feature, per the constitution.

## Analysis Remediation (2026-11-04)

Applied from `/speckit-analyze` with the recommended fix for each finding:

- F1 (MEDIUM, coverage): FR-010 names concurrent Deletes, but T018 only raced Puts; T018 now interleaves Deletes.
- F2 (MEDIUM, underspecification): Delete's `meta/` sync failure was in T020 but not in the spec; added US5 scenario 4 (error returned, data file kept).
- F3 (MEDIUM, ambiguity): SC-002 "bounded by a fixed buffer" had no number; it now says less than 4 MiB allocated for 64 MiB, matching T008.
- F4 (LOW, inconsistency): T007/T008 and T014–T016 were marked `[P]` while sharing a file; T008 moved to `memory_test.go`, `[P]` removed from T014–T016.
- F5 (LOW): the quickstart wrote to a fixed `/tmp` path; it now uses `mktemp`.

No constitution conflicts were found. Coverage: 17/17 functional requirements and 6/6 success criteria map to at least one task.
