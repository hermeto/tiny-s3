# Tasks: Minimal Bucket Support

**Input**: Design documents from `/specs/006-minimal-bucket-support/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required by the constitution (Principles IV and V); every test task is TDD-first and must be observed failing before its implementation task.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Make the real `aws s3api` CLI available to `make e2e`.

- [X] T001 Create `e2e/Dockerfile`: `ARG GO_IMAGE`, `FROM ${GO_IMAGE} AS go`, `FROM amazon/aws-cli:2.31.11@sha256:7df59f7bf15fd670eef5a08dfd4b1da6f4c1de8edd9e74b19574d1db1794a884`, copy `/usr/local/go`, set `PATH`, `GOPATH=/go`, `CGO_ENABLED=0`, empty `ENTRYPOINT` (research R9)
- [X] T002 Change the `e2e` target in `Makefile` to build `e2e/Dockerfile` as `tiny-s3-e2e` with `--build-arg GO_IMAGE=$(GO_IMAGE)` and run the tests in it with the same mounts and environment as `DOCKER_RUN` (keep GNU make 3.81 compatibility)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Bucket entity, name validation, S3 error codes and the shared temp-record writer that every story needs.

- [X] T003 [P] Write failing table tests for `ValidBucketName` in `internal/storage/bucket_test.go` covering every rule: "3–63 characters; lowercase letters, digits, dots and hyphens; starts and ends with a letter or digit; no two adjacent dots; not formatted as an IPv4 address; not starting with `xn--`, `sthree-` or `amzn-s3-demo-`; not ending with `-s3alias`, `--ol-s3`, `.mrap`, `--x-s3` or `--table-s3`", with valid names at each boundary (3 and 63 chars, dots and hyphens inside, digits-only, `1.2.3` and `1.2.3.4.5`)
- [X] T004 Implement `ValidBucketName`, `Bucket`, `ErrInvalidBucketName`, `ErrNoSuchBucket`, `ErrBucketExists` in `internal/storage/bucket.go`
- [X] T005 [P] Write failing tests for the new APIError values (`ErrInvalidBucketName`, `ErrNoSuchBucket`, `ErrBucketAlreadyOwnedByYou`, `ErrMalformedXML`, `ErrInvalidLocationConstraint`: status, code, message per `contracts/http-buckets.md`) in `internal/s3api/errors_test.go`, then add them to `internal/s3api/errors.go`
- [X] T006 Extract the temp-file write of `writeRecord` into `writeTemp(pattern string, b []byte) (string, error)` in `internal/storage/put.go`; existing storage tests must stay green (refactor, no behavior change)

**Checkpoint**: `make test` passes; names validate.

---

## Phase 3: User Story 1 - Create a bucket that survives a restart (Priority: P1) 🎯 MVP

**Goal**: `aws s3api create-bucket` answers 200, re-create answers 409, invalid names and regions answer 400, and buckets are reloaded at startup.

**Independent Test**: Create `demo`, restart the server on the same data directory, and see it again with the same creation date.

### Tests for User Story 1

- [X] T007 [P] [US1] Write failing tests in `internal/storage/bucket_test.go`: `Open` creates `buckets/`; `CreateBucket` returns the name and the store clock in UTC; re-create → `ErrBucketExists` with the original date unchanged; invalid name → `ErrInvalidBucketName` with nothing written; reopen loads the bucket with the same date; record content `{"v":1,"name":...,"created":...}`
- [X] T008 [P] [US1] Write failing fault tests in `internal/storage/bucket_fault_test.go`: the successful create history matches `contracts/storage-buckets.md`; a failure injected at each step before the rename leaves no bucket (in memory and after reopen) and an empty `tmp/`; a `buckets/` open/sync/close failure after the rename rolls back (bucket absent after reopen); a rollback failure returns both errors and leaves the bucket present; an unencodable creation date (year 10000) fails with `encode bucket`
- [X] T009 [P] [US1] Write failing load tests in `internal/storage/bucket_load_test.go`: each bad entry fails `Open` with `ErrCorruptRecord` naming the path ("unreadable, over 64 KiB, does not decode, unknown version, name different from its file name, zero creation date", invalid file name, missing `.json`); `ReadDir` and `Open` failures on `buckets/` are returned
- [X] T010 [P] [US1] Write a failing concurrency test in `internal/storage/bucket_test.go`: 16 goroutines create the same bucket; exactly one succeeds, the rest get `ErrBucketExists` (run under `-race`)
- [X] T011 [P] [US1] Write failing CreateBucket handler tests in `internal/s3api/bucket_test.go` with a fake `BucketStore`: 200 + `Location: /<bucket>`; no body, whitespace body, `LocationConstraint` absent/empty/`us-east-1` → created; other location → 400 `InvalidLocationConstraint`; bad XML, wrong root element, body over 64 KiB (65,536 bytes accepted, 65,537 rejected), body read error → 400 `MalformedXML`; store errors → 400 `InvalidBucketName`, 409 `BucketAlreadyOwnedByYou`, 500 `InternalError` logged as `bucket_error` with the request ID
- [X] T012 [P] [US1] Write failing tests in `internal/app/app_test.go`: `Main` serves CreateBucket on a real data dir; a `storage.Open` failure (injected with `fsx.Faulty` on `MkdirAll` of `buckets`) exits 1 with `s3d: storage:` on stderr and never logs `listening`

### Implementation for User Story 1

- [X] T013 [US1] Implement `CreateBucket`, `loadBuckets` and the bucket map/mutex in `internal/storage/bucket.go`, and add `buckets/` plus the load step to `Open` in `internal/storage/storage.go` (research R1–R4)
- [X] T014 [US1] Implement `BucketStore`, `BucketOps(store, log)` and the CreateBucket Op in `internal/s3api/bucket.go` (research R7)
- [X] T015 [US1] Open the store in `run` and wire `s3api.BucketOps` in `Main` in `internal/app/app.go` (research R8); update `TestRunForcedShutdown` to the new handler constructor
- [X] T016 [US1] Add `startProcIn(t, dir)` to `e2e/shutdown_test.go` and write `e2e/buckets_test.go` with an `awsCLI` helper (throwaway credentials, temp `HOME`, `--endpoint-url`, JSON output; fails if `aws` is not on `PATH`) and tests: create → exit 0 and `Location`; re-create → `BucketAlreadyOwnedByYou`; `Bad_Name` → `InvalidBucketName`; `LocationConstraint=eu-west-1` → `InvalidLocationConstraint`; restart on the same data dir keeps the bucket and its creation date

**Checkpoint**: US1 works end to end with the real CLI.

---

## Phase 4: User Story 2 - Check that a bucket exists (Priority: P1)

**Goal**: `head-bucket` answers 200 or 404.

**Independent Test**: Head an existing and a missing bucket with the CLI.

### Tests for User Story 2

- [X] T017 [P] [US2] Write failing tests for `HeadBucket` in `internal/storage/bucket_test.go`: existing → the `Bucket`; missing → `ErrNoSuchBucket`; invalid → `ErrInvalidBucketName`
- [X] T018 [P] [US2] Write failing gate tests in `internal/s3api/handler_test.go`: with `Ops.CheckBucket` set, HEAD/GET-list/PUT-object/GET/HEAD/DELETE on a bucket run the Op only when the check passes and otherwise answer its error (HEAD without body); `PUT /<bucket>` skips the check; `GET /` and routing errors skip it; a nil Op behind a passing check still answers 501. In the same task, update the expectations the gate changes so they fail first: `e2e/routing_test.go` (HEAD `/missing/k` → 404 without body; `GET /b/k` → 400 `InvalidBucketName`, since `b` is too short; replace the `GET /` 501 row with `GET /b?acl` 501), the in-flight request of `e2e/shutdown_test.go` (now a complete 404 `NoSuchBucket`) and `TestMainServesAndStops` in `internal/app/app_test.go` (GET `/bucket/key` → 404 `NoSuchBucket`)
- [X] T019 [P] [US2] Write failing tests for `CheckBucket` and the HeadBucket Op in `internal/s3api/bucket_test.go`: existing → 200 with `x-amz-bucket-region: us-east-1` and no body; `ErrNoSuchBucket` → 404 `NoSuchBucket`; `ErrInvalidBucketName` → 400 `InvalidBucketName`; other error → 500 `InternalError` logged as `bucket_error`

### Implementation for User Story 2

- [X] T020 [US2] Implement `HeadBucket` in `internal/storage/bucket.go`
- [X] T021 [US2] Add `Ops.CheckBucket` and the gate to `internal/s3api/handler.go` (research R6); implement `CheckBucket` and the HeadBucket Op in `internal/s3api/bucket.go`
- [X] T022 [US2] Add e2e tests to `e2e/buckets_test.go`: `head-bucket` on an existing bucket → exit 0; on a missing one → `(404)`; on `Bad_Name` → `(400)`

**Checkpoint**: US1 and US2 work independently.

---

## Phase 5: User Story 4 - Object operations require an existing bucket (Priority: P1)

**Goal**: Every object operation on a missing bucket answers 404 `NoSuchBucket`.

**Independent Test**: Run the five object commands against a never-created bucket.

- [X] T023 [US4] Write an integration test in `internal/app/app_test.go` through `Main` with a real data dir: PUT/GET/DELETE object and `GET /<bucket>?list-type=2` on a missing bucket → 404 `NoSuchBucket`, HEAD object → 404 without body, GET object on a created bucket → 501. US4 is delivered by the shared gate (T021), so this test verifies the wiring through the real store and may pass on its first run
- [X] T024 [US4] Add e2e tests to `e2e/buckets_test.go`: `put-object`, `get-object`, `delete-object`, `list-objects-v2` on a missing bucket → `NoSuchBucket`; `head-object` → `(404)`; `get-object` on an existing bucket → `NotImplemented`

**Checkpoint**: Gate verified with the real CLI.

---

## Phase 6: User Story 3 - List buckets (Priority: P2)

**Goal**: `list-buckets` returns every bucket with its creation date, sorted by name.

**Independent Test**: Create `beta` then `alpha`, list, see `alpha`, `beta` with dates.

### Tests for User Story 3

- [X] T025 [P] [US3] Write failing tests for `ListBuckets` in `internal/storage/bucket_test.go`: empty store → empty slice; buckets sorted by name; the returned slice is a copy
- [X] T026 [P] [US3] Write failing tests for the ListBuckets Op in `internal/s3api/bucket_test.go`: XML per `contracts/http-buckets.md` (namespace, owner, `<Buckets>` present when empty, order, `CreationDate` UTC with milliseconds), `Content-Type: application/xml`

### Implementation for User Story 3

- [X] T027 [US3] Implement `ListBuckets` in `internal/storage/bucket.go` and the ListBuckets Op in `internal/s3api/bucket.go`
- [X] T028 [US3] Add e2e tests to `e2e/buckets_test.go`: empty `list-buckets` → empty `Buckets`; after creating `beta` and `alpha` → both in name order with `CreationDate`; the restart test also compares `list-buckets` before and after

**Checkpoint**: All stories work.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T029 Update `README.md`: intro and quick start (bucket commands with the aws CLI), `make e2e` row (aws CLI image), layout (`internal/storage` wired, `buckets/`), and a `### 006 minimal bucket support` design section with this feature's trade-offs (research R1–R9)
- [X] T030 Run `make vet test race cover e2e` and confirm 100% coverage for every `internal/` package
- [X] T031 Run `quickstart.md` against `docker compose up -d --build` with the host aws CLI, then `docker compose down -v`

---

## Dependencies & Execution Order

- Setup (T001–T002) blocks only the e2e tasks (T016, T022, T024, T028).
- Foundational (T003–T006) blocks all stories.
- US1 (T007–T016) is the MVP; US2 (T017–T022) needs the store from T013 and `BucketOps` from T014; US4 (T023–T024) needs the gate from T021; US3 (T025–T028) needs T013–T014.
- T018 changes existing e2e and app expectations together with the new gate tests, so the suite is never left asserting the pre-gate behavior after T021.
- Polish runs last.

Within each story, test tasks come first and must fail before the implementation task that follows.

## Parallel Opportunities

- T003 and T005 (different packages).
- US1 tests T007–T012 are in different files and can be written together (T007 and T010 share `bucket_test.go`; write them in one pass).
- US2 tests T017, T018, T019; US3 tests T025, T026.

```text
# US1 tests in parallel
T008 internal/storage/bucket_fault_test.go
T009 internal/storage/bucket_load_test.go
T011 internal/s3api/bucket_test.go
T012 internal/app/app_test.go
```

## Implementation Strategy

MVP is US1 (create plus restart), then US2 and US4 (the gate, which object features depend on), then US3. Each checkpoint runs `make test`; the full gate and the quickstart run once at the end.

## Analysis Remediation (2026-11-04)

`/speckit-analyze` findings, each fixed with the recommended option as authorized for this feature:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| I1 | MEDIUM | FR-007 said "every request naming a bucket", but requests rejected by routing (subresource, method, `InvalidURI`) keep their errors (research R6, contract). | FR-007 now names the bucket and object operations and states that routing errors are unchanged. |
| I2 | MEDIUM | T023 updated e2e and app expectations after the gate (T021) had already changed them, leaving the suite red in between and the expectation change not test-first. | Moved those updates into T018 (before T021); T023 is now a `Main`-level verification test for US4. |
| U1 | MEDIUM | Quickstart step 3 expected "`BucketRegion` ... or nothing", which is not checkable. | Step 3 now expects `"BucketRegion": "us-east-1"`. |
| U2 | LOW | "Body over 64 KiB" left the boundary open. | Contract and T011 state 65,536 bytes accepted, 65,537 rejected. |

No constitution conflicts. Coverage: FR-001–FR-013 and SC-001–SC-005 each map to at least one task (100%).
