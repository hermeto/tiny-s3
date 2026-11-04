# Implementation Plan: Durable Object Storage

**Branch**: `005-durable-object-storage` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/005-durable-object-storage/spec.md`

## Summary

Add `internal/storage`, an HTTP-independent storage engine with `Put`, `Get`, `Head` and `Delete` addressed by bucket and key. Each Put streams the body through a fixed 32 KiB buffer into a temp file while computing MD5, validates length and digest, then commits in the constitution's order: file sync, rename into `data/<version>`, `data/` sync, then a metadata record written to a temp file, synced, renamed over `meta/<sha256>.json` and followed by a `meta/` sync. The metadata rename is the commit point. Every data version is immutable and uniquely named, so open readers keep their version through overwrite and delete. A striped per-key mutex serializes commits, deletes and the Get lookup so concurrent writers end with exactly one complete winner. `internal/fsx` gains read-side operations (`Open`, `ReadDir`) and matching fault kinds so every engine branch is covered by fault injection.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned container image)

**Primary Dependencies**: Go standard library only (`crypto/md5`, `crypto/sha256`, `crypto/rand`, `encoding/json`, `sync`)

**Storage**: Local filesystem under a store root: `tmp/`, `data/`, `meta/`

**Testing**: `go test`, `go test -race`, `go vet`, `scripts/cover-gate.sh` (100% per `internal/` package), existing e2e suite

**Target Platform**: Linux container (production); macOS hosts only run Docker

**Project Type**: Go server; this feature adds an internal library package

**Performance Goals**: Put throughput bounded by disk, not by the engine; one 32 KiB copy buffer per Put

**Constraints**: Bounded memory independent of object size; no partial object ever visible; single process per data directory

**Scale/Scope**: Objects up to 5 GiB; flat `data/` and `meta/` directories (no fan-out) — see Complexity Tracking and research R6

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Durability Before Acknowledgement**: PASS. Put returns only after body temp write, file sync, rename into `data/`, `data/` sync, and the metadata record's temp write, sync, rename and `meta/` sync. The metadata record is the commit point; an object without it does not exist. Every failure removes the temp file and any data file the Put created; a `meta/` sync failure after the rename rolls the record back to the previous state.
- **II. Streaming, Bounded Memory**: PASS. One 32 KiB buffer per Put via `io.CopyBuffer`; the writer is an `io.MultiWriter` of the temp file and the MD5 hash, which has no `ReaderFrom` shortcut that could bypass the buffer. A test bounds allocations for a 64 MiB Put.
- **III. Own Storage Engine, Standard Library First**: PASS. New code is `internal/storage` and `internal/fsx`, standard library only.
- **IV. Test-First, 100% Coverage**: PASS. Every task writes the failing test first. All I/O failures go through `fsx.Faulty`, including a generic test that injects a failure at every operation recorded in a successful Put. `make cover` covers the new package.
- **V. Real-Client Compatibility**: PASS (not triggered). No S3 operation is added or changed; the engine is not yet wired to HTTP, so no new `aws s3api` e2e test applies. Error identities match the S3 codes the HTTP layer will render (IncompleteBody, BadDigest, EntityTooLarge, KeyTooLongError, NoSuchKey).
- **Scope and Constraints**: PASS. Index rebuild and LIST stay out of scope; Get/Head read the metadata record from disk.
- **Development Workflow**: PASS. One branch and one PR; README design section gains the feature's trade-offs.

Post-design re-check: PASS, no change.

## Project Structure

### Documentation (this feature)

```text
specs/005-durable-object-storage/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── storage-api.md
│   └── fsx-read-ops.md
├── checklists/requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
internal/fsx/
├── fsx.go          # + ReadFile, FS.Open, FS.ReadDir, OS adapter
├── faulty.go       # + OpOpen, OpRead, OpReadDir, read-file wrapper
└── *_test.go

internal/storage/
├── storage.go      # Store, Open, Info, Reader, errors, validation
├── put.go          # streaming write path and commit
├── read.go         # Get, Head, metadata record decoding
├── delete.go       # Delete
├── lock.go         # striped key locks
└── *_test.go       # internal tests (package storage): storage, put, memory,
                    # read, fault, concurrency, delete
```

**Structure Decision**: One new package `internal/storage` beside `internal/s3api`; the HTTP layer will depend on it in the PutObject feature. The engine is not wired into `internal/app` in this feature (research R9).

## Design Decisions

See [research.md](research.md) for rationale and alternatives. Summary:

- **R1 layout**: `<root>/tmp/` for in-flight files, `<root>/data/<version>` for immutable bodies, `<root>/meta/<hex sha256(bucket "/" key)>.json` for records. Bucket names cannot contain `/`, so the hash input is unambiguous, and the hashed name keeps every key inside `meta/`.
- **R2 commit order**: body temp → sync → close → rename to `data/<version>` → sync `data/` → record temp → sync → close → [lock] read previous record → rename to `meta/` → sync `meta/` → remove previous data (best effort) [unlock].
- **R3 rollback**: if the `meta/` sync fails after the rename, re-commit the previous record (or remove the new record when there was none), then remove the new data file; the original error is returned, joined with any rollback error.
- **R4 concurrency**: 64 striped mutexes keyed by the record hash. Commit, Delete and Get's record-read-plus-data-open hold the stripe; Head does not need it because a record rename is atomic.
- **R5 input checks**: copy exactly `size` bytes with `io.CopyBuffer` over an `io.LimitReader`, then probe one more byte; short → IncompleteBody, extra → IncompleteBody. Context cancellation is checked on every read.
- **R6 flat directories**: no hash fan-out; accepted for the target scale and recorded in the README.
- **R7 crash leftovers**: `Open` removes everything in `tmp/`. Orphaned `data/` files from a crash between data rename and record commit are invisible and left for the index-rebuild feature.
- **R8 fsx read ops**: `Open` returns `ReadFile` (`Name`, `Read`, `ReadAt`, `Seek`, `Close`); `ReadDir` lists a directory. `Faulty` injects `OpOpen`, `OpRead` (Read and ReadAt) and `OpReadDir`.
- **R9 wiring**: not wired into `internal/app`; the PutObject feature wires it.

## Complexity Tracking

No constitution violations. No complexity exception is required.
