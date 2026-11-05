# Tasks: Range Requests

**Input**: Design documents from `/specs/010-range-requests/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required (constitution IV and V); every test task is written first and observed failing.

## Phase 1: Setup

No setup needed.

---

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T001 [P] Write failing table tests for `parseRange` in `internal/s3api/rangereq_test.go`: every row of `contracts/http-range.md` (serve, clamp, suffix longer than the object, `bytes=N-`, `bytes=-0`, size 0, multiple ranges, `bytes=abc`, `items=0-1`, `bytes=5-3`, 64-bit overflow, missing both numbers, upper-case `BYTES`, spaces around the value and `-`)
- [ ] T002 [P] Write failing table tests for `ifRangeMatches` in `internal/s3api/rangereq_test.go`: quoted and unquoted current ETag match; other ETag, weak ETag and garbage do not; the last-modified date (whole seconds) matches; any other date does not
- [ ] T003 [P] Write a failing test for `ErrInvalidRange` (416, `InvalidRange`, "The requested range is not satisfiable") in `internal/s3api/errors_test.go`, then add it to `internal/s3api/errors.go`
- [ ] T004 Implement `parseRange` and `ifRangeMatches` in `internal/s3api/rangereq.go` (research R1, R2)

---

## Phase 3: User Story 1 - Read part of an object (Priority: P1) 🎯 MVP

- [ ] T005 [P] [US1] Write failing handler tests in `internal/s3api/get_test.go` (real store): GET with `bytes=0-99`, `bytes=100-`, `bytes=-50`, clamped end, long suffix and `bytes=0-` (206, not 200) → 206, exact bytes, `Content-Range`, `Content-Length` = range length and the 009 headers; HEAD with the same ranges → same status and headers, no body; a read fault inside the range aborts the connection
- [ ] T006 [US1] Apply `parseRange` in GET (section reader) and HEAD in `internal/s3api/object.go` (research R3)
- [ ] T007 [US1] Write `e2e/range_test.go`: `aws s3api get-object --range` with the three forms and a clamped end, outputs equal to the source slices, `ContentRange` as expected

---

## Phase 4: User Story 2 - Unsatisfiable ranges (Priority: P1)

- [ ] T008 [P] [US2] Write failing handler tests in `internal/s3api/get_test.go`: `bytes=N-`, `bytes=-0` and any range on an empty object → 416 `InvalidRange` with `Content-Range: bytes */N` (HEAD without body); 412/304 from 009 conditions win over the range
- [ ] T009 [US2] Answer unsatisfiable ranges in `internal/s3api/object.go`
- [ ] T010 [US2] Add to `e2e/range_test.go`: `--range bytes=<size>-` → `(InvalidRange)`

---

## Phase 5: User Story 3 - Ranges S3 ignores (Priority: P2)

- [ ] T011 [P] [US3] Write failing handler tests in `internal/s3api/get_test.go`: multiple and malformed ranges → 200 whole object without `Content-Range`; `If-Range` stale ETag, weak ETag or other date → 200; current ETag or date → 206
- [ ] T012 [US3] Apply `If-Range` in GET and HEAD in `internal/s3api/object.go`
- [ ] T013 [US3] Add to `e2e/range_test.go`: CLI multiple and malformed ranges return the whole object; Go-client `If-Range` stale → 200 and current → 206; extend the 2 GiB test in `e2e/objects_test.go` with `bytes=1073741824-` (1 GiB, MD5 of the second half, peak RSS below 64 MiB)

---

## Phase 6: Polish

- [ ] T014 Add the `### 010 range requests` design section to `README.md` and update the intro and quick start
- [ ] T015 Run `make vet test race cover e2e`; 100% for every `internal/` package
- [ ] T016 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

- T001–T004 block the stories; US1 → US2 → US3 all edit `object.go` in turn.

## Parallel Opportunities

- T001/T002 (one new file) with T003 (errors).

## Implementation Strategy

Parser first, then serve ranges (MVP), then 416, then the cases S3 ignores.

## Analysis Remediation (2026-11-04)

`/speckit-analyze` findings, fixed with the recommended option:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| I1 | MEDIUM | US1 scenario 4 ("`bytes=10-` followed by an end beyond N") was ambiguous and untestable as written. | Rewritten as `bytes=10-M` with M ≥ N and the expected `Content-Range`. |
| C1 | LOW | The edge case "`bytes=0-` answers 206, not 200" had no task. | Added to T005. |

No constitution conflicts. Coverage: FR-001–FR-009 and SC-001–SC-003 each map to at least one task (100%).
