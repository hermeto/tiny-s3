# Tasks: Filesystem Failure Injection

**Input**: Design documents from `/specs/004-filesystem-failure-injection/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, quickstart.md

**Tests**: Required by the specification and constitution; every test task is TDD-first and must be observed failing before its implementation task.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm the existing seam and establish the implementation inventory.

- [X] T001 Inventory all current filesystem call sites and map each to `internal/fsx` operations in `internal/fsx/fsx.go` and `internal/app/datadir.go`; document the absent object-storage caller in `specs/004-filesystem-failure-injection/plan.md`
- [X] T002 [P] Add operation constants and typed history/rule names to `internal/fsx/fsx.go` with comments documenting the eight supported operation kinds

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Establish the narrow real-filesystem interfaces and preserve current startup behavior before adding fault policy.

- [X] T003 [P] Write failing OS-adapter tests for create, write, file sync, rename, remove, directory create, directory open, and directory sync in `internal/fsx/fsx_test.go`
- [X] T004 Extend `FS`, file-handle, and directory-handle interfaces plus `OS` implementations in `internal/fsx/fsx.go`, delegating directly to the operating-system calls and preserving native errors
- [X] T005 Update `internal/app/datadir.go` and `internal/app/datadir_test.go` to use the extended interfaces without changing startup error or cleanup semantics

**Checkpoint**: The real adapter compiles and existing application tests still pass; user-story work can begin.

---

## Phase 3: User Story 1 - Prove Storage Failure Handling (Priority: P1) 🎯 MVP

**Goal**: Tests can inject each supported failure, target a path pattern, select the Nth matching call, and force a short write.

**Independent Test**: Run the `internal/fsx` fault suite and the application/storage failure tests; each configured failure returns the injected error, earlier calls pass through, and short writes are not acknowledged as complete.

### Tests for User Story 1

- [X] T006 [P] [US1] Write failing tests for first-call injection and path filtering for create, write, file sync, rename, remove, directory create, directory open, and directory sync in `internal/fsx/faulty_test.go`
- [X] T007 [P] [US1] Write failing tests for Nth matching calls, nonmatching calls, first-rule precedence, invalid glob patterns, and unused rules in `internal/fsx/faulty_test.go`
- [X] T008 [P] [US1] Write failing tests for injected short writes with nil and nonnil errors, including `io.ErrShortWrite` handling at the abstraction boundary in `internal/fsx/faulty_test.go`
- [X] T009 [P] [US1] Write failing failure-path tests proving temporary-file cleanup and original-error preservation for the existing data-directory probe in `internal/app/datadir_test.go`

### Implementation for User Story 1

- [X] T010 Implement validated glob path matching, Nth matching-call counters, and deterministic first-rule precedence in `internal/fsx/faulty.go`
- [X] T011 Implement fault-aware file and directory handles for write, file sync, directory open, directory sync, rename, remove, and directory creation in `internal/fsx/faulty.go`
- [X] T012 Implement short-write injection and standard short-write normalization in `internal/fsx/faulty.go`
- [X] T013 Route every current disk operation through `fsx.FS` and preserve cleanup/error semantics in `internal/app/datadir.go`; leave the same seam ready for later storage operations

**Checkpoint**: All eight operation failures, Nth matching behavior, path filtering, short writes, and cleanup are independently proven by unit tests.

---

## Phase 4: User Story 2 - Verify Durable Operation Ordering (Priority: P1)

**Goal**: The fake exposes an immutable, ordered operation history that proves successful and failed durability sequences.

**Independent Test**: Execute a successful write and assert the history contains write, file sync, rename, and parent-directory sync in order; inject each failure and assert the failing operation is present.

### Tests for User Story 2

- [X] T014 [P] [US2] Write failing history assertions for operation kind, primary/secondary paths, byte counts, result, and immutable snapshots in `internal/fsx/faulty_test.go`
- [X] T015 [P] [US2] Write failing operation-order and failure-stop assertions for write, file sync, rename, cleanup, and directory sync in `internal/fsx/faulty_test.go`

### Implementation for User Story 2

- [X] T016 Implement synchronized ordered history recording before delegation and final outcome capture in `internal/fsx/faulty.go`
- [X] T017 Expose copy-safe history snapshots for callers and use them in ordering tests in `internal/fsx/faulty.go` and `internal/fsx/faulty_test.go`
- [X] T018 Prove the filesystem abstraction can record the required write/sync/rename/directory-sync sequence and expose failure outcomes for future storage callers in `internal/fsx/faulty_test.go`, while preserving current cleanup behavior in `internal/app/datadir.go` and `internal/app/datadir_test.go`

**Checkpoint**: Ordering and failure visibility are independently assertable without inspecting private fake state.

---

## Phase 5: User Story 3 - Preserve Production Behavior and Coverage (Priority: P2)

**Goal**: Production uses `fsx.OS` with unchanged behavior, and all internal code reaches 100% coverage under the required gates.

**Independent Test**: Run `make vet test race cover e2e` and the quickstart Compose smoke check; all commands pass and the coverage gate reports no uncovered internal statements.

### Tests for User Story 3

- [X] T019 [P] [US3] Add regression tests comparing real-adapter behavior and native error propagation for all supported operations in `internal/fsx/fsx_test.go`
- [X] T020 [P] [US3] Add concurrency tests for rule counters and history snapshots under `-race` in `internal/fsx/faulty_test.go`

### Implementation for User Story 3

- [X] T021 Remove or cover every remaining uncovered branch in `internal/fsx` and affected `internal/app` packages, keeping `scripts/cover-gate.sh` at 100% in `internal/`
- [X] T022 Update `README.md` Design with the abstraction, glob/Nth semantics, history behavior, short-write trade-off, and production-adapter choice
- [X] T023 Run and fix the complete quickstart validation, including `make vet test race cover e2e`, `docker compose up -d --build`, applicable AWS CLI v2 commands, and `docker compose down -v`

**Checkpoint**: Production and fake implementations are both covered, race-safe, documented, and validated end to end.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final traceability and release readiness.

- [X] T024 Review all completed tasks against FR-001 through FR-012 and SC-001 through SC-005, then update `specs/004-filesystem-failure-injection/tasks.md` with `[X]` markers in the implementation phase
- [X] T025 Run `gofmt` on changed Go files and inspect `git diff --check` before the feature commit/PR
- [X] T026 Verify every feature commit is GPG-signed and record the verification method in `specs/004-filesystem-failure-injection/quickstart.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies.
- **Foundational (Phase 2)**: Depends on Setup; blocks all user stories.
- **User Stories (Phases 3-5)**: Depend on Foundational. US2 depends on the fault-aware handles from US1; US3 depends on US1 and US2 behavior being stable.
- **Polish (Phase 6)**: Depends on all implementation and validation work.

### User Story Dependencies

- **US1 (P1)**: Starts after Phase 2; MVP and prerequisite for US2.
- **US2 (P1)**: Starts after US1's fault-aware handles; verifies the storage durability sequence.
- **US3 (P2)**: Starts after US1/US2; closes coverage, documentation, and e2e gates.

### Parallel Opportunities

- T003 and T002 can run in parallel because they touch separate test/source concerns; T004 waits for T003.
- T006-T009 can run in parallel because they are separate test concerns/files, then T010-T013 implement the shared behavior.
- T014 and T015 can run in parallel after US1; T016-T018 follow.
- T019 and T020 can run in parallel after US2.

## Implementation Strategy

1. Complete the existing `fsx` seam without changing app behavior.
2. Deliver US1 as the MVP with tests written and observed failing before implementation.
3. Add history/order assertions in US2.
4. Finish production regression, README trade-offs, coverage, race, Compose, and e2e validation in US3.
5. Mark tasks `[X]` immediately when each task is completed and re-run `speckit-converge` before opening the PR.
