# Tasks: DeleteObject

**Input**: Design documents from `/specs/011-delete-object/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required (constitution IV and V); every test task is written first and observed failing.

## Phase 1: Setup

No setup needed.

## Phase 2: Foundational

- [ ] T001 Extend `ObjectStore` with `Delete(bucket, key) error` in `internal/s3api/object.go` and add it to the `fakeObjects` double in `internal/s3api/object_test.go`

---

## Phase 3: User Story 1 - Delete an object (Priority: P1) 🎯 MVP

- [ ] T002 [P] [US1] Write failing handler tests in `internal/s3api/delete_test.go` (real store): DELETE of an existing key → 204, no body, only `x-amz-request-id`; then GET/HEAD → 404 `NoSuchKey`; DELETE of a missing key → 204; twice → 204; 1025-byte key → 400 `KeyTooLongError`; a corrupt record is removed with 204; `If-Match` with another ETag is ignored (204)
- [ ] T003 [US1] Implement `DeleteObjectOp` in `internal/s3api/object.go`; wire it in `internal/app/app.go`; update `TestMainObjectOpsNeedBucket` in `internal/app/app_test.go`
- [ ] T004 [US1] Write `e2e/delete_test.go`: put, `aws s3api delete-object` twice (exit 0 both), then `head-object` → `(404)` and `get-object` → `(NoSuchKey)`; missing bucket → `(NoSuchBucket)`

---

## Phase 4: User Story 2 - Deletes are durable (Priority: P1)

- [ ] T005 [P] [US2] Write a failing handler test in `internal/s3api/delete_test.go`: with `fsx.Faulty` failing the `meta/` directory sync, DELETE → 500 `InternalError` logged as `storage_error`; after reopening the store, the object is readable with its original bytes
- [ ] T006 [US2] Make T005 pass (expected to need no code beyond T003; fix the mapping if it does)
- [ ] T007 [US2] Add to `e2e/delete_test.go`: put, delete with the CLI, SIGKILL s3d right after the CLI returns, restart on the same data dir; `head-object` → `(404)`

---

## Phase 5: User Story 3 - Downloads in progress are not cut (Priority: P2)

- [ ] T008 [US3] Extend `TestGetSurvivesOverwrite` in `e2e/objects_test.go` with an HTTP DELETE after the overwrite; the download still returns the original bytes and a later GET answers 404

---

## Phase 6: Polish

- [ ] T009 Add the `### 011 DeleteObject` design section to `README.md` and update the intro and quick start
- [ ] T010 Run `make vet test race cover e2e`; 100% for every `internal/` package
- [ ] T011 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

T001 → US1 (MVP) → US2 → US3 → Polish. US2 and US3 only add tests on top of T003.

## Parallel Opportunities

T002 and T005 are in one new file; T004/T007 and T008 are different e2e files.

## Implementation Strategy

Wire the operation, then prove durability and read consistency.
