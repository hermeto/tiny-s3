# Feature Specification: Filesystem Failure Injection

**Feature Branch**: `004-filesystem-failure-injection`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "A filesystem abstraction for tiny-s3 so that every disk operation the storage engine performs can be made to fail on demand in tests. Developers must be able to prove, with unit tests, what happens when creating a file, writing, fsyncing a file, renaming, removing, creating a directory, opening a directory or fsyncing a directory fails, including failures on the Nth call and short writes. Production code uses the real OS filesystem with identical behavior. Tests can inject an error for a specific operation and path pattern, on the Nth matching call. The fake records the sequence of operations, so tests can assert ordering (for example: write, sync, rename, then directory sync). 100% coverage."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Prove storage failure handling (Priority: P1)

As a storage-engine developer, I can cause each disk operation to fail at a chosen point so that cleanup, error translation, and durability behavior are verified by unit tests.

**Why this priority**: Failure handling is essential to the durability guarantee and is the primary value of the feature.

**Independent Test**: Configure one operation and path pattern to fail on its first matching call, exercise the storage path, and verify the returned error and resulting filesystem state.

**Acceptance Scenarios**:

1. **Given** a configured create, write, file-sync, rename, remove, directory-create, directory-open, or directory-sync failure, **When** the storage engine performs that matching operation, **Then** the configured error is returned and no later operation is reported as successful.
2. **Given** a configured failure on the Nth matching call, **When** fewer than N matching calls occur, **Then** all earlier calls behave like the real filesystem; **When** the Nth call occurs, **Then** it fails with the configured error.
3. **Given** a write configured to return fewer bytes than requested, **When** the storage engine writes a body, **Then** it detects the short write and follows its normal failure cleanup path.

---

### User Story 2 - Verify durable operation ordering (Priority: P1)

As a storage-engine developer, I can inspect the fake filesystem's operation history so that tests prove the required order of writes, file sync, rename, and parent-directory sync.

**Why this priority**: Correct ordering is part of the durability contract, not merely an implementation detail.

**Independent Test**: Run a successful write through the fake filesystem and assert the recorded operation sequence, including paths and operation kinds.

**Acceptance Scenarios**:

1. **Given** a successful object write, **When** the operation completes, **Then** the history shows write, file sync, rename, and parent-directory sync in that order, with any required create/open operations also represented.
2. **Given** a failure at any recorded step, **When** the storage engine handles the failure, **Then** the history stops at the failing operation except for explicitly required cleanup operations.

---

### User Story 3 - Preserve production behavior and coverage (Priority: P2)

As a maintainer, I can run production with the real operating-system filesystem and run tests with the fake implementation without changing storage semantics.

**Why this priority**: The abstraction must improve testability without changing behavior in production.

**Independent Test**: Run the complete unit suite against the real filesystem adapter and the fault-injection suite against the fake, including race and coverage checks.

**Acceptance Scenarios**:

1. **Given** the production configuration, **When** the server starts and handles filesystem-backed operations, **Then** it uses the operating-system filesystem and retains existing behavior.
2. **Given** the repository test suite, **When** coverage is measured, **Then** every package under `internal/` reaches 100% statement coverage and all tests pass with the race detector.

### Edge Cases

- A path pattern matches multiple operations but only the configured operation kind counts toward the Nth-call threshold.
- A path pattern matches no calls; the configured failure remains unused and all operations succeed normally.
- The Nth matching call fails after earlier matching calls succeeded; nonmatching paths remain unaffected.
- A short write reports zero or a positive count smaller than the requested buffer, with or without an error, and the storage engine must not treat it as a successful complete write.
- Directory handles must be closed through the same abstraction where the storage engine currently closes them; close behavior is not a new injectable failure in this feature unless required by the existing interface.
- Removing a temporary file during failure cleanup must be observable and must not mask the original storage error.
- Multiple configured rules are evaluated deterministically in registration order, with the first matching rule deciding the injected result.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The storage engine MUST perform all filesystem operations through one abstraction boundary, while production configuration uses the real operating-system filesystem.
- **FR-002**: The abstraction MUST cover creating files, writing bytes, syncing files, renaming paths, removing paths, creating directories, opening directories, and syncing directories.
- **FR-003**: The test filesystem MUST allow a test to inject a specified error for one operation kind and a path pattern.
- **FR-004**: The test filesystem MUST apply an injection only on the configured Nth matching call; calls before that threshold MUST retain normal behavior.
- **FR-005**: The test filesystem MUST support an injected short write that reports fewer bytes than requested and allows the caller to observe the resulting write behavior.
- **FR-006**: The test filesystem MUST record each attempted operation in order, including operation kind, relevant path(s), and whether it succeeded or failed.
- **FR-007**: The recorded history MUST be available to unit tests without requiring inspection of private implementation state.
- **FR-008**: The storage engine MUST preserve the existing durability ordering: body write, file sync, atomic rename, and parent-directory sync before acknowledging success.
- **FR-009**: Any failure in the write path MUST retain the existing cleanup and error semantics, including attempting to remove the temporary file without replacing the original error.
- **FR-010**: The abstraction MUST preserve the existing production behavior and error behavior when backed by the real operating-system filesystem.
- **FR-011**: Unit tests MUST cover every supported operation's injected failure, Nth-call behavior, path filtering, short writes, operation ordering, cleanup, and production-adapter behavior.
- **FR-012**: Every package under `internal/` MUST reach 100% statement coverage, and the full suite MUST pass with the race detector.

### Key Entities

- **Filesystem**: The operation boundary used by the storage engine, with real and test-backed behaviors.
- **Fault Rule**: A test configuration identifying an operation kind, path pattern, threshold call number, and injected result.
- **Operation Record**: An ordered history entry describing an attempted filesystem operation, its paths, and outcome.
- **Short Write**: A test-controlled write result that reports fewer bytes than requested so callers' handling can be verified.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of statements in every package under `internal/` are covered by automated tests.
- **SC-002**: Automated tests exercise injected failures for all eight supported operation kinds, at first call and at a later matching call, with zero uncovered failure branches.
- **SC-003**: At least one automated test proves the successful durability sequence includes write, file sync, rename, and directory sync in that order.
- **SC-004**: At least one automated test proves a short write cannot be acknowledged as a complete successful storage operation and triggers the documented cleanup/error path.
- **SC-005**: Production behavior remains unchanged when the real operating-system filesystem is selected, demonstrated by the existing unit, race, and end-to-end checks passing.

## Assumptions

- The feature is internal to the storage engine and does not add a public S3 API or change client-visible success semantics.
- The existing storage engine's supported filesystem operations define the initial abstraction scope; directory handle close remains governed by the existing design unless the implementation requires exposing it.
- Path patterns use a deterministic, documented matching rule selected during planning; the recommended default is glob-style matching suitable for temporary and final object paths.
- Fault rules are test-only configuration and are not enabled by production configuration.
- The repository's existing Makefile targets are the source of truth for vetting, tests, race detection, coverage, and e2e validation.

## Decisions Recorded

- Recommended scope: cover exactly the eight failure categories listed in the request and include operation history and short-write injection.
- Recommended threshold semantics: count only matching operation kind and path calls; fail exactly on the Nth matching call.
- Recommended rule precedence: first matching rule in registration order.
- Recommended pattern behavior: glob-style path matching, with exact paths representable as patterns.
- Recommended clarification handling: no user clarification was needed because the request and project constitution provide sufficient defaults; these defaults are recorded above.
