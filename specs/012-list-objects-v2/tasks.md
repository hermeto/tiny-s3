# Tasks: Ordered Index and ListObjectsV2

**Input**: Design documents from `/specs/012-list-objects-v2/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required (constitution IV and V); every test task is written first and observed failing.

## Phase 1: Setup

No setup needed.

---

## Phase 2: Foundational (Blocking Prerequisites)

- [X] T001 [P] Write failing tests for `objectIndex` in `internal/storage/index_test.go`: put keeps keys sorted in UTF-8 byte order (including non-ASCII and keys that are prefixes of each other), put of an existing key replaces its entry without duplicating it, remove of present and missing keys, buckets are independent, an empty bucket disappears
- [X] T002 Implement `objectIndex` in `internal/storage/index.go` (research R1)
- [X] T003 [P] Write failing tests in `internal/storage/index_test.go` for keeping the index equal to the records: after Put, overwrite, Delete and Delete of a missing key the index matches `Head`; with `fsx.Faulty`, a failed record rename leaves the index unchanged, a `meta/` sync failure rolled back restores the previous entry (or removes the new one), a rollback that fails keeps the new entry, a failed record removal keeps the entry, and a failed `meta/` sync after a removal drops it (research R2)
- [X] T004 Update the index in `commit`/`rollback` (`internal/storage/put.go`) and `Delete` (`internal/storage/delete.go`) under the key lock
- [X] T005 [P] Write failing tests in `internal/storage/index_test.go` for the rebuild: reopening a store lists every object with its size, MD5 and last-modified; an undecodable record, a record whose bucket and key do not hash to its file name, and a non-`.json` entry are skipped and `Open` succeeds; a `ReadDir`, `Open` or read failure on `meta/` fails `Open` (`fsx.Faulty`)
- [X] T006 Rebuild the index in `Open` in `internal/storage/storage.go` (research R3)

**Checkpoint**: the index matches the records in every unit test.

---

## Phase 3: User Story 1 - List a bucket (Priority: P1) 🎯 MVP

- [X] T007 [P] [US1] Write failing tests for `Store.List` in `internal/storage/list_test.go`: all keys in order with size, MD5 and last-modified; empty and unknown buckets return nothing; `MaxKeys` 0 returns an empty, non-truncated result; a listing makes no filesystem call (`fsx.Faulty` history stays empty, FR-007)
- [X] T008 [US1] Implement `Store.List` (without prefix and delimiter yet) in `internal/storage/list.go`
- [X] T009 [P] [US1] Write failing handler tests in `internal/s3api/list_test.go`: XML per `contracts/http-list-objects-v2.md` (namespace, Name, MaxKeys, KeyCount, IsTruncated false, Contents with `LastModified` in milliseconds UTC, quoted ETag, Size, `STANDARD`), `Content-Type: application/xml`; `max-keys` invalid (`-1`, `abc`) and `encoding-type=xml` → 400 `InvalidArgument`
- [X] T010 [US1] Implement `ListObjectsV2Op` in `internal/s3api/list.go` and wire it in `internal/app/app.go`; update `TestMainObjectOpsNeedBucket` in `internal/app/app_test.go`
- [X] T011 [US1] Write `e2e/list_test.go`: keys uploaded in random order list in byte order with every field through `aws s3api list-objects-v2`; an empty bucket; `(NoSuchBucket)`; a listing right after a put and after a delete reflects them (the check 011 deferred here); the listing survives a restart

---

## Phase 4: User Story 2 - Browse by prefix and folders (Priority: P1)

- [X] T012 [P] [US2] Write failing tests in `internal/storage/list_test.go`: prefix filtering; delimiter `/` and a multi-character delimiter group keys into common prefixes, each emitted once and counted in `MaxKeys`; a key equal to the prefix; a prefix with no match
- [X] T013 [US2] Add prefix and delimiter to `Store.List` in `internal/storage/list.go` (research R4, binary-search jump past each group)
- [X] T014 [P] [US2] Write failing tests in `internal/s3api/list_test.go`: `Prefix`, `Delimiter` and `CommonPrefixes` in the XML; `encoding-type=url` encodes keys, prefixes, delimiter and start-after with unreserved characters and `/` kept (space `%20`, `+` `%2B`, `%` `%25`, UTF-8 bytes `%XX`)
- [X] T015 [US2] Implement the prefix/delimiter parameters and URL encoding in `internal/s3api/list.go`
- [X] T016 [US2] Add to `e2e/list_test.go`: prefix and delimiter through `aws s3api`; `aws s3 ls s3://demo/dir/` shows `PRE sub/` and the files; keys with spaces, `+`, `%` and non-ASCII list exactly as stored

---

## Phase 5: User Story 3 - Page through large listings (Priority: P1)

- [X] T017 [P] [US3] Write failing tests in `internal/storage/list_test.go`: `MaxKeys` pages with `Truncated` and `Next`; resuming after a key and after a whole common prefix (`AfterGroup`) when a group spans a page boundary; `After` before, inside and past the keys
- [X] T018 [US3] Add `After`, `AfterGroup`, `Truncated` and `Next` to `Store.List` in `internal/storage/list.go`
- [X] T019 [P] [US3] Write failing tests in `internal/s3api/list_test.go`: default and capped `max-keys` (1000), `NextContinuationToken` and `ContinuationToken` echo, token precedence over `start-after` (which is still echoed), `start-after` alone, undecodable tokens → `InvalidArgument`, and a concurrent test under `-race` that pages through 2,500 keys while goroutines add, overwrite and delete other keys, expecting every original key exactly once
- [X] T020 [US3] Implement tokens and `start-after` in `internal/s3api/list.go` (research R4)
- [X] T021 [US3] Add to `e2e/list_test.go`: 2,500 keys paged by hand with `aws s3api list-objects-v2 --no-paginate --max-keys 100 [--continuation-token T]` while a concurrent HTTP client uploads new keys spread across the key space and overwrites original keys; every original key exactly once and no key twice; `--start-after` through the CLI; the CLI's own paginator (`--page-size 100`, no `--max-keys`) returns all 2,500 keys

---

## Phase 6: Polish

- [X] T022 Write a timing test in `internal/storage/list_timing_test.go` (build tag `!race`, so it runs in `make test` and `make cover` but not under the race detector's 5–10× slowdown) that lists 1,000 keys from 10,000 and fails above 100 ms (SC-004)
- [X] T023 Add the `### 012 ordered index and ListObjectsV2` design section to `README.md` and update the intro and quick start
- [X] T024 Run `make vet test race cover e2e`; 100% for every `internal/` package
- [X] T025 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

- Phase 2 (index, mirroring, rebuild) blocks the stories. US1 → US2 → US3 extend `list.go` in both packages in turn.

## Parallel Opportunities

- T001/T003/T005 share `index_test.go`; storage tests and s3api tests of each story are in different packages and can be written together.

## Implementation Strategy

Index first, kept equal to the records; then plain listing (MVP), prefixes and folders, and paging.

## Analysis Remediation (2026-11-05)

`/speckit-analyze` findings, fixed with the recommended option:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| C1 | MEDIUM | FR-007 (no disk reads per listing) had no assertion. | T007 asserts that `List` makes no filesystem call. |
| C2 | MEDIUM | The rebuild had no rule for a record that decodes but sits under the wrong file name, or for non-`.json` entries; indexing them could list a key twice or under the wrong bucket. | T005 skips both, as corrupt; research R3 updated. |
| I1 | MEDIUM | T021 mixed the CLI's own paginator (`--starting-token`, `--page-size`) with manual paging; the CLI turns auto-pagination off when a manual token or page limit is given, so the test would not have controlled the pages. | T021 pages by hand with `--no-paginate --max-keys 100 --continuation-token`, and checks the CLI's paginator separately with `--page-size 100`. |
| U1 | MEDIUM | "Every key exactly once under concurrent writes" did not say which writes, nor which keys the assertion covers. | T021 and T019 name them: new keys spread across the key space and overwrites of original keys; every original key exactly once, no key twice. |
| C3 | MEDIUM | SC-004's 100 ms bound would be measured under `-race` in `make race`, where it is meaningless and flaky. | T022 uses the `!race` build tag: the bound is checked by `make test` and `make cover`, not under instrumentation. |
| I2 | LOW | T011 asked to update a 006 e2e test that only checks `list-objects-v2` on a missing bucket (still `NoSuchBucket`). | Clause removed; the 011 deferral (deleted keys are not listed) is named in T011 instead. |

No constitution conflicts. Coverage: FR-001–FR-010 and SC-001–SC-004 each map to at least one task (100%).

## Implementation Notes (2026-11-05)

- Rebuilding the index in `Open` made the storage test helper `reopen` apply its faults during `Open`'s new record reads. `reopen` now opens cleanly and applies the faults to later operations, which is what every caller meant; one s3api test uses `Nth: 2` for the same reason.
- The CLI's paginator merges pages and drops `KeyCount`; e2e checks of per-page fields use `--no-paginate`.
- The timing test (T022) passed on its first run; it guards SC-004 against regressions.

## Phase 7: Convergence

- [ ] T026 Write a timing test in `internal/s3api/list_timing_test.go` (build tag `!race`) that serves `GET /demo?list-type=2` through `ListObjectsV2Op` over a store with 10,000 keys and requires the full 1,000-entry XML response within 100 ms per SC-004 (partial)
