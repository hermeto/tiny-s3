# Implementation Plan: GetObject and HeadObject

**Branch**: `009-get-head-object` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-get-head-object/spec.md`

## Summary

Serve GetObject and HeadObject from the 005 engine's `Get` and `Head`: one header writer for ETag, length, type, last-modified, `Accept-Ranges` and user metadata; S3/HTTP conditional evaluation (412/304); a 32 KiB streaming copy that aborts the connection on any failure after the status. The engine's immutable readers give consistency under concurrent overwrite and delete.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned image)

**Primary Dependencies**: Standard library only

**Storage**: Unchanged

**Testing**: `go test`, `-race`, `go vet`, cover gate, `make e2e`

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: Download throughput bounded by disk and network; one 32 KiB buffer per download

**Constraints**: Peak RSS below 64 MiB for a 2 GiB download; never a short 200

**Scale/Scope**: Objects up to 5 GiB

## Constitution Check

- **I. Durability**: PASS (read path only; nothing written).
- **II. Streaming, Bounded Memory**: PASS. Fixed 32 KiB copy buffer; 2 GiB RSS e2e check.
- **III. Standard Library First**: PASS.
- **IV. Test-First, 100% Coverage**: PASS. Read failures through `fsx.Faulty` (`OpOpen`, `OpRead`); tests first.
- **V. Real-Client Compatibility**: PASS. `aws s3api get-object`/`head-object` e2e; S3 codes and statuses.
- **Scope / Workflow**: PASS. Ranges are 010; one PR; README design section.

Post-design re-check: PASS.

## Project Structure

```text
specs/009-get-head-object/  spec plan research data-model quickstart contracts/http-get-head-object.md tasks

internal/s3api/
├── object.go          # ObjectStore += Get, Head; GetObjectOp, HeadObjectOp, header writer
├── conditional.go     # evaluateConditions, ETag list matching
├── errors.go          # NoSuchKey, PreconditionFailed
├── storageerr.go      # ErrNoSuchKey mapping
└── *_test.go
internal/app/app.go    # wire GetObject, HeadObject
e2e/objects_test.go    # get/head with the CLI, conditionals, 2 GiB download, overwrite during GET
README.md              # 009 design section
```

## Design Decisions

See [research.md](research.md): R1 interface, R2 headers, R3 conditionals, R4 streaming and aborts, R5 errors, R6 memory and consistency proofs.

## Complexity Tracking

No constitution violations.
