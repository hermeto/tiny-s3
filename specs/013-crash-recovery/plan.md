# Implementation Plan: Crash Recovery and Concurrency Guarantees

**Branch**: `013-crash-recovery` | **Date**: 2026-11-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/013-crash-recovery/spec.md`

## Summary

Make `storage.Open` a full recovery pass: empty `tmp/`, rebuild the index from valid records whose body exists, remove unreferenced bodies, and report the counts; `s3d` logs them as one `recovery` line before listening. Prove the guarantees with a SIGKILL during 20 parallel uploads, 50 concurrent PUTs to one key, and a randomized fault-injection test of the engine.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned image)

**Primary Dependencies**: Standard library only

**Storage**: Unchanged format; recovery at `Open`

**Testing**: `go test`, `-race`, `go vet`, cover gate, `make e2e` (real SIGKILL)

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: Startup reads each record once and lists `data/` once

**Constraints**: Fail startup on any recovery I/O error; one process per data directory

**Scale/Scope**: Startup time grows with the number of objects (one small record read each)

## Constitution Check

- **I. Durability Before Acknowledgement**: PASS. Recovery relies on the 005 order and never makes an unacknowledged object visible: bodies without a record are removed, records without a body are skipped.
- **II. Streaming, Bounded Memory**: PASS. Startup memory grows with the object count (index and the set of referenced bodies), as the in-memory index already does; no body is read.
- **III. Standard Library First**: PASS.
- **IV. Test-First, 100% Coverage**: PASS. Every recovery I/O failure through `fsx.Faulty`; a randomized fault-injection test; tests first.
- **V. Real-Client Compatibility**: PASS. The crash and concurrency e2e tests verify with `aws s3api`.
- **Scope**: PASS. "The index is held in memory and rebuilt from the durable metadata records at startup."

Post-design re-check: PASS.

## Project Structure

```text
specs/013-crash-recovery/  spec plan research data-model quickstart contracts/recovery.md tasks

internal/storage/
├── recovery.go        # RecoveryReport, Recovery(), orphan scan and removal
├── storage.go         # Open runs the recovery steps in order
├── index.go           # loadIndex: skip body-less records, return referenced bodies
└── recovery_test.go   # counts, skips, failures, randomized crash test
internal/app/app.go    # log the recovery line
e2e/recovery_test.go   # SIGKILL during 20 uploads; 50 concurrent PUTs
README.md              # 013 design section
```

## Design Decisions

See [research.md](research.md): R1 crash leftovers, R2 startup sequence, R3 logging, R4 proofs, R5 limits.

## Complexity Tracking

No constitution violations.
