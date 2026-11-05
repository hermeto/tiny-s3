# Implementation Plan: DeleteObject

**Branch**: `011-delete-object` | **Date**: 2026-11-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/011-delete-object/spec.md`

## Summary

Wire the 005 engine's `Delete` to HTTP as `DeleteObjectOp` (204, shared error mapping) and prove the guarantees end to end: idempotency with the aws CLI, a SIGKILL right after an acknowledged delete, a delete during a download, and a failing `meta/` sync that leaves the object readable.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned image)

**Primary Dependencies**: Standard library only

**Storage**: Unchanged (005 delete path)

**Testing**: `go test`, `-race`, `go vet`, cover gate, `make e2e`

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: One record removal and one directory sync per delete

**Constraints**: Acknowledge only after the removal is durable

**Scale/Scope**: Single-object deletes

## Constitution Check

- **I. Durability Before Acknowledgement**: PASS. 204 only after the engine removed the record and synced `meta/`; a sync failure answers 500 and the data file is kept.
- **II. Streaming**: PASS (no body).
- **III. Standard Library First**: PASS.
- **IV. Test-First, 100% Coverage**: PASS. Failure through `fsx.Faulty`; tests first.
- **V. Real-Client Compatibility**: PASS. `aws s3api delete-object` e2e, including a crash.
- **Scope / Workflow**: PASS.

Post-design re-check: PASS.

## Project Structure

```text
specs/011-delete-object/  spec plan research data-model quickstart contracts/http-delete-object.md tasks

internal/s3api/object.go     # ObjectStore += Delete; DeleteObjectOp
internal/s3api/delete_test.go
internal/app/app.go          # wire DeleteObject
e2e/delete_test.go           # CLI deletes, SIGKILL after delete
e2e/objects_test.go          # HTTP delete during a download
README.md                    # 011 design section
```

## Design Decisions

See [research.md](research.md): R1 wiring, R2 crash proof, R3 delete during download, R4 scope.

## Complexity Tracking

No constitution violations.
