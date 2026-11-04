# Research: Filesystem Failure Injection

## Decision 1: Extend the existing `internal/fsx` seam

- **Decision**: Keep one `fsx.FS` boundary and extend it for storage operations.
- **Rationale**: The repository already injects `fsx.FS` into `internal/app`; a second mocking seam would let production paths bypass the testable boundary and violate the constitution.
- **Alternatives considered**: Call-site mocks were rejected because they cannot prove the actual operation sequence; a package-global fake was rejected because it creates test coupling and race hazards.

## Decision 2: Use explicit file and directory handles

- **Decision**: Represent opened handles with interfaces that expose only the operations the storage engine needs, including `Write`, `Sync`, `Close`, and directory sync.
- **Rationale**: `*os.File` implements both file and directory behavior, but separate narrow interfaces make unsupported use visible and make fake history precise. The OS adapter preserves native errors.
- **Alternatives considered**: Exposing `*os.File` was rejected because tests would bypass injection; a generic `any` handle was rejected because it weakens compile-time guarantees.

## Decision 3: Glob path matching and Nth matching calls

- **Decision**: Rules match an operation kind plus `path.Match` pattern, count only matching calls, and inject on exactly the configured Nth match.
- **Rationale**: Glob patterns cover exact paths and temporary-name families without inventing a custom matcher. Matching-call semantics make rules stable when unrelated operations occur.
- **Alternatives considered**: Prefix matching was rejected because it cannot express sibling exclusions; regular expressions were rejected as more powerful than needed and easier to misuse.

## Decision 4: Record attempted calls with outcomes

- **Decision**: Append a history record before delegation and fill its result after delegation or injection.
- **Rationale**: A failed operation is still an attempted operation and must appear in ordering assertions. Recording before delegation also preserves the exact call order under failures.
- **Alternatives considered**: Recording only successful calls was rejected because it hides the operation that caused a failure; logging strings were rejected because typed assertions are less brittle.

## Decision 5: Short write semantics

- **Decision**: The fake can return a configured byte count below the input length; the storage caller converts a nil-error short write to `io.ErrShortWrite` using standard `io` semantics.
- **Rationale**: Go writers are permitted to report short writes, and callers must not acknowledge incomplete data. The behavior is deterministic and directly testable.
- **Alternatives considered**: Splitting writes inside the fake was rejected because it would test retry policy rather than the storage engine's response to a writer result.

## Decision 6: State synchronization

- **Decision**: Protect mutable rule counters and operation history with a mutex.
- **Rationale**: The race gate is mandatory and the storage engine may perform independent filesystem work concurrently in future paths; the fake should remain safe for those tests.
- **Alternatives considered**: Requiring tests to serialize access was rejected because it makes the fake less faithful and hides races in test instrumentation.
