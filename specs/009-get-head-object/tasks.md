# Tasks: GetObject and HeadObject

**Input**: Design documents from `/specs/009-get-head-object/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required (constitution IV and V); every test task is written first and observed failing.

## Phase 1: Setup

No setup needed.

---

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T001 [P] Write failing tests for `ErrNoSuchKey` (404, "The specified key does not exist.") and `ErrPreconditionFailed` (412, "At least one of the pre-conditions you specified did not hold") in `internal/s3api/errors_test.go`, and for `storage.ErrNoSuchKey` → `ErrNoSuchKey` in `internal/s3api/storageerr_test.go`; then add them to `internal/s3api/errors.go` and `internal/s3api/storageerr.go`
- [ ] T002 Extend `ObjectStore` with `Get` and `Head` in `internal/s3api/object.go` and the `fakeObjects` test double in `internal/s3api/object_test.go` (stored objects kept in memory so Get/Head can return them)

**Checkpoint**: `make test` green.

---

## Phase 3: User Story 1 - Download an object (Priority: P1) 🎯 MVP

- [ ] T003 [P] [US1] Write failing handler tests in `internal/s3api/object_test.go` (real store): GET returns 200, the exact bytes, `ETag`, `Content-Length`, `Content-Type`, `Last-Modified` in `http.TimeFormat` truncated to the second, `Accept-Ranges: bytes`, `x-amz-meta-<name>` per entry; zero-byte object; pre-007 record reports `binary/octet-stream`; missing key → 404 `NoSuchKey`
- [ ] T004 [P] [US1] Write failing tests in `internal/s3api/object_test.go` for failures: `Get` failing with an I/O error before the status → 500 `InternalError`; a read failure mid-body (`fsx.Faulty` `OpRead` on the data file) aborts the connection (client gets an error or a short read against `Content-Length`, never a clean short 200) and is logged as `storage_error`; the reader is closed in every case
- [ ] T005 [US1] Implement `GetObjectOp` and the header writer in `internal/s3api/object.go` (research R2, R4); wire it in `internal/app/app.go`; update `TestMainObjectOpsNeedBucket` in `internal/app/app_test.go` (GET on an existing bucket is now 404 `NoSuchKey`)
- [ ] T006 [US1] Add e2e tests to `e2e/objects_test.go`: put then `aws s3api get-object` of 0 bytes, 11 bytes and 10 MiB random files, byte-identical outputs; the CLI's JSON shows `ContentType`, `ContentLength`, `ETag`, `LastModified`, `AcceptRanges`, `Metadata`; a missing key → `(NoSuchKey)`; update the 006 e2e test that expected `NotImplemented` for get-object on an existing bucket

---

## Phase 4: User Story 2 - Fetch metadata only (Priority: P1)

- [ ] T007 [P] [US2] Write failing handler tests in `internal/s3api/object_test.go`: HEAD returns the same status and headers as GET and no body; missing key → 404 without body; I/O error → 500 without body
- [ ] T008 [US2] Implement `HeadObjectOp` in `internal/s3api/object.go`; wire it in `internal/app/app.go`; update `TestMainObjectOpsNeedBucket`
- [ ] T009 [US2] Add e2e tests to `e2e/objects_test.go`: `aws s3api head-object` fields equal get-object's; missing key → `(404)`

---

## Phase 5: User Story 3 - Conditional requests (Priority: P2)

- [ ] T010 [P] [US3] Write failing table tests for `evaluateConditions` in `internal/s3api/conditional_test.go`: every row of the contract table, `*`, lists, weak and unquoted ETags, whole-second comparison (a date equal to the truncated last-modified is "not modified"), unparseable dates ignored, the two S3 special cases (If-Match true + If-Unmodified-Since false → 200; If-None-Match false + If-Modified-Since true → 304)
- [ ] T011 [P] [US3] Write failing handler tests in `internal/s3api/object_test.go`: GET and HEAD with conditions → 304 (ETag and Last-Modified, no body, no Content-Length) and 412 (`PreconditionFailed`, HEAD without body); missing key with conditions → 404; the reader is closed on 304/412
- [ ] T012 [US3] Implement `internal/s3api/conditional.go` and use it in GET and HEAD
- [ ] T013 [US3] Add e2e tests to `e2e/objects_test.go`: `--if-none-match <etag>` → `(304)`; `--if-match '"0000"'` → `(PreconditionFailed)`; `--if-modified-since` in the future → `(304)`; `--if-match <etag>` → success

---

## Phase 6: User Story 4 - Large and concurrent downloads (Priority: P2)

- [ ] T014 [P] [US4] Write a handler test in `internal/s3api/object_test.go` on a real server and store: start a GET of a 1 MiB object, read 64 KiB, overwrite the key with other bytes and delete it through the engine, read the rest; the body equals the original
- [ ] T015 [US4] Extend `TestPutObjectLargeFlatMemory` in `e2e/objects_test.go` to download the 2 GiB object, compare its MD5 and keep the peak RSS below 64 MiB; add an e2e test where an HTTP overwrite lands while a GET is half read and the GET still returns the original bytes

---

## Phase 7: Polish

- [ ] T016 Add the `### 009 GetObject and HeadObject` design section to `README.md` (research R1–R6) and update the intro and quick start
- [ ] T017 Run `make vet test race cover e2e`; 100% for every `internal/` package
- [ ] T018 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

- T001–T002 block the stories. US1 (T003–T006) is the MVP; US2 reuses the header writer; US3 plugs into both ops; US4 needs GET.

## Parallel Opportunities

- T003/T004, T007, T010/T011 and T014 are test tasks in separate test functions; T010 is its own file.

## Implementation Strategy

GET first (MVP), then HEAD, conditionals, and the large/concurrent proofs.

## Analysis Remediation (2026-11-04)

`/speckit-analyze` findings, fixed with the recommended option:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| I1 | MEDIUM | US4 AC2 says the key is overwritten and then deleted during a download, but HTTP DeleteObject is feature 011, so the e2e test (T015) can only overwrite. | Spec Assumptions state that the delete is proved through the engine in T014 and that 011 extends the e2e test. |
| C1 | LOW | The 006 tests that expect `NotImplemented` for GET/HEAD on an existing bucket would turn red after T005/T008. | Already part of T005, T006 and T008; no change. |

No constitution conflicts. Coverage: FR-001–FR-011 and SC-001–SC-004 each map to at least one task (100%).
