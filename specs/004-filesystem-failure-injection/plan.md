# Implementation Plan: Filesystem Failure Injection

**Branch**: `004-filesystem-failure-injection` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/004-filesystem-failure-injection/spec.md`

## Summary

Extend the existing `internal/fsx` boundary from data-directory probing to the complete set of storage-engine filesystem operations. Add a real OS adapter with identical semantics, a stateful fault-injecting fake with glob path matching, Nth-match rules, short-write results, and ordered operation history, then route the storage engine through it and prove every failure path with TDD. Update the README with the trade-offs and provide a runnable quickstart for the validation gates.

## Technical Context

**Language/Version**: Go 1.27.3, standard library production dependencies only

**Primary Dependencies**: Go standard library; existing `internal/fsx` package; AWS CLI v2 only for e2e where applicable

**Storage**: Local filesystem under the configured data directory; durable object files and metadata are part of later storage paths

**Testing**: `go test`, `go test -race`, `go vet`, statement coverage gate, Docker Compose e2e

**Target Platform**: Linux server in the pinned Go/Docker environment; development also runs on macOS through Docker

**Project Type**: Go server with internal storage and S3 protocol packages

**Performance Goals**: The production adapter adds no extra policy or buffering beyond the OS calls; the fake is test-only and need not optimize for production throughput

**Constraints**: Every internal package must remain at 100% statement coverage; object bodies remain streamed; durable write ordering must be preserved; no third-party production modules

**Scale/Scope**: One process and one local data directory; this feature changes the internal I/O seam and its tests, not the public S3 surface

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Durability Before Acknowledgement**: PASS. The abstraction explicitly represents file sync, rename, and directory sync so the storage path can test the required order and cleanup.
- **II. Streaming, Bounded Memory**: PASS. The interface exposes `io.Writer`-compatible file writes and does not buffer object bodies.
- **III. Own Storage Engine, Standard Library First**: PASS. The implementation stays in `internal/fsx` and uses the standard library.
- **IV. Test-First, 100% Coverage**: PASS. Tasks require failing tests before implementation, fault injection at the abstraction boundary, race checks, and the existing coverage gate.
- **Real-Client Compatibility**: PASS. No S3 protocol behavior changes; the existing e2e gate remains required.
- **Development Workflow**: PASS. One branch/PR, signed logical commits, README trade-off documentation, and all required gates before PR.

## Project Structure

```text
internal/fsx/
├── fsx.go              # FS, File, directory and OS adapter contracts
├── faulty.go           # fault rules, path matching, operation history and fake wrapper
└── *_test.go           # real adapter, fake behavior, ordering, Nth calls, short writes

internal/app/
├── datadir.go          # existing filesystem seam retained
└── *_test.go           # failure cleanup and startup behavior

specs/004-filesystem-failure-injection/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
└── tasks.md
```

**Structure Decision**: Extend the already-established `internal/fsx` package rather than introducing a second mock layer. Storage callers receive the `fsx.FS` dependency, while `fsx.OS` remains the production adapter and `fsx.Faulty` is used only by unit tests.

## Design Decisions

- Use explicit operation methods for file creation, write, file sync, rename, remove, directory creation, directory open, and directory sync. Explicit methods make history and fault selection unambiguous.
- Model open files and directories as small interfaces that expose `Name`, `Write`, `Sync`, `Close`, and directory `Sync`; the OS adapter wraps `*os.File` without changing returned errors.
- Use glob-style path matching via `path.Match`, with an exact path represented by a literal pattern and an empty pattern matching all paths. Invalid patterns are rejected when a rule is constructed.
- Count only calls matching both operation kind and path pattern. A rule with `Nth: 0` is normalized to the first matching call; rules fire once at the configured Nth call and otherwise delegate.
- Record attempted operations before invoking the underlying adapter, with outcome updated after return, so a failed call is visible and ordering assertions include the failure.
- Inject short writes through a write result (`n < len(p)` and optional error); storage code must treat a short write without an error as `io.ErrShortWrite`.
- Keep rule evaluation and history state behind a mutex so tests can safely inspect history while exercising concurrent code under `-race`.

## Complexity Tracking

No constitution violations. No complexity exception is required.

## Phase 0 Research Summary

See [research.md](research.md). All design unknowns are resolved there.

## Phase 1 Design Summary

See [data-model.md](data-model.md) for the entities and state, and [quickstart.md](quickstart.md) for runnable validation. No public S3 contract changes, so no new `contracts/` artifact is needed.
