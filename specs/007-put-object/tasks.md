# Tasks: PutObject

**Input**: Design documents from `/specs/007-put-object/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required by the constitution (Principles IV and V); every test task is written first and observed failing.

## Phase 1: Setup

No setup: the e2e image with the aws CLI exists since 006.

---

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T001 [P] Write failing tests in `internal/storage/put_test.go`: `Put` with `PutOptions{ContentType, Metadata}` returns them in `Info`, `Head` and `Get` return them, an overwrite without metadata clears it, and an empty `ContentType` is stored as "`binary/octet-stream` when the request has none"; a record without `content_type`/`metadata` written by hand reads as `binary/octet-stream` with no metadata
- [ ] T002 [P] Write a failing test in `internal/storage/put_test.go`: a body reader returning `io.ErrUnexpectedEOF` before the declared length fails with `ErrIncompleteBody` and leaves `tmp/` and `data/` empty
- [ ] T003 Change `Store.Put` to take `PutOptions` (`ContentMD5`, `ContentType`, `Metadata`) in `internal/storage/put.go` and `internal/storage/storage.go`; add `content_type`/`metadata` (`omitempty`) to the record and `ContentType`/`Metadata` to `Info` in `internal/storage/read.go`; map `io.ErrUnexpectedEOF` to `ErrIncompleteBody` in `copyBody`; update every existing `Put` call in `internal/storage/*_test.go`
- [ ] T004 [P] Write failing tests for the new APIError values (`MissingContentLength` 411, `InvalidDigest`, `MetadataTooLarge`, `KeyTooLongError`, `EntityTooLarge`, `IncompleteBody`, `BadDigest`, `InvalidArgument`, all 400, messages per `contracts/http-put-object.md`) in `internal/s3api/errors_test.go`, then add them to `internal/s3api/errors.go`
- [ ] T005 Write failing tests in `internal/s3api/storageerr_test.go` for the shared mapping (every row of research R6, `context.Canceled` → `IncompleteBody`, unknown → 500 logged as `storage_error` with request ID, op, bucket, key), then add `internal/s3api/storageerr.go` and switch the bucket operations to it (rename the `bucket_error` log to `storage_error` in `internal/s3api/bucket_test.go`)

**Checkpoint**: `make test` green; the store keeps content type and metadata.

---

## Phase 3: User Story 1 - Upload an object and get its ETag (Priority: P1) 🎯 MVP

**Goal**: `aws s3api put-object` returns 200 with the MD5 ETag.

**Independent Test**: Upload a file and compare the ETag with its MD5.

- [ ] T006 [P] [US1] Write failing handler tests in `internal/s3api/object_test.go` with a fake `ObjectStore`: 200 with `ETag: "<md5>"` and empty body; the store receives bucket, key, declared size and the body bytes; overwrite; zero-length body; keys with `/`, spaces, `+` and UTF-8 reach the store unchanged
- [ ] T007 [P] [US1] Write a failing integration test in `internal/app/app_test.go` through `Main`: create a bucket, PUT an object, then after stopping open the store on the same data dir and `Head` the key (size, MD5)
- [ ] T008 [US1] Implement `ObjectStore` and `PutObjectOp` in `internal/s3api/object.go`; wire `ops.PutObject` in `internal/app/app.go`
- [ ] T009 [US1] Write `e2e/objects_test.go`: `aws s3api put-object` of a file to `a/b.txt` returns the quoted MD5 ETag; overwrite returns the new ETag; missing bucket → `(NoSuchBucket)`

**Checkpoint**: Upload works with the real CLI.

---

## Phase 4: User Story 2 - Keep content type and user metadata (Priority: P1)

- [ ] T010 [P] [US2] Write failing tests in `internal/s3api/object_test.go`: `Content-Type` and `x-amz-meta-*` reach the store (lowercase names, repeats joined with `,`), absent content type passes as empty (the store applies the default), metadata of exactly 2048 bytes accepted and 2049 rejected with `MetadataTooLarge` before the store is called
- [ ] T011 [US2] Implement metadata collection and the 2048-byte limit in `internal/s3api/object.go`
- [ ] T012 [US2] Extend the T007 integration test in `internal/app/app_test.go` to send `Content-Type` and two metadata headers and check them in the reopened store; add an e2e upload with `--content-type` and `--metadata` in `e2e/objects_test.go`

---

## Phase 5: User Story 3 - Reject corrupt or incomplete uploads (Priority: P1)

- [ ] T013 [P] [US3] Write failing tests in `internal/s3api/object_test.go`: aws-chunked (`Content-Encoding: aws-chunked`, and `x-amz-content-sha256: STREAMING-UNSIGNED-PAYLOAD-TRAILER`) → 501 without reading the body; no length (`Transfer-Encoding: chunked`) → 411; `Content-MD5` not base64 or not 16 bytes → `InvalidDigest`; a valid `Content-MD5` reaches the store as raw bytes; store errors map per R6; none of the rejected requests reads the body
- [ ] T014 [P] [US3] Write a failing test in `internal/s3api/object_test.go` on a real `httptest.Server` with the real store: a raw request declaring 100 bytes that sends 10 and half-closes gets 400 `IncompleteBody`, and the key does not exist afterwards
- [ ] T015 [US3] Implement the aws-chunked, length and `Content-MD5` checks in `internal/s3api/object.go` (research R4 order)
- [ ] T016 [US3] Add e2e tests to `e2e/objects_test.go`: wrong `--content-md5` → `(BadDigest)`; 1025-byte key → `(KeyTooLongError)`; raw early disconnect against the binary → 400 `IncompleteBody`; raw `Transfer-Encoding: chunked` → 411 `MissingContentLength`

---

## Phase 6: User Story 4 - Large objects with flat memory (Priority: P2)

- [ ] T017 [P] [US4] Write a failing handler test in `internal/s3api/object_test.go`: a declared length above 5 GiB → 400 `EntityTooLarge` without reading the body (real store)
- [ ] T018 [US4] Add a 2 GiB e2e test in `e2e/objects_test.go`: stream a generated body with a Go client, compare the ETag with the client-side MD5, and require `VmHWM` of the s3d process below 64 MiB (research R7)

---

## Phase 7: Polish

- [ ] T019 Add the `### 007 PutObject` design section to `README.md` (research R1–R7) and update the intro and quick start
- [ ] T020 Run `make vet test race cover e2e`; 100% coverage for every `internal/` package
- [ ] T021 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

- Foundational T001–T005 block every story (T003 changes the `Put` signature everything uses).
- US1 (T006–T009) is the MVP; US2–US4 build on `PutObjectOp` from T008.
- T017 needs T008; T018 needs T008 wired in `app`.

## Parallel Opportunities

- T001/T002 (storage) with T004 (s3api).
- Within each story, the `[P]` test tasks touch different files or are written in one pass.

## Implementation Strategy

Foundational first (signature change), then US1 as MVP, then metadata, rejections and the large-upload check; the full gate and quickstart once at the end.
