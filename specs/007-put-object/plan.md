# Implementation Plan: PutObject

**Branch**: `007-put-object` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/007-put-object/spec.md`

## Summary

Wire the 005 storage engine to HTTP for PutObject. `s3api.PutObjectOp` validates the request (aws-chunked, length, `Content-MD5`, user metadata) before reading the body, then streams the body into `Store.Put`, which now also stores content type and user metadata in the record. Storage errors map to S3 errors in one shared function. The aws CLI e2e tests cover the acceptance criteria, and a 2 GiB e2e upload checks the server's peak RSS.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned container image)

**Primary Dependencies**: Go standard library only; e2e uses the AWS CLI v2 from the 006 e2e image

**Storage**: Existing store layout; the object record gains `content_type` and `metadata`

**Testing**: `go test`, `-race`, `go vet`, `scripts/cover-gate.sh`, `make e2e`

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: Upload throughput bounded by disk; one 32 KiB copy buffer per upload

**Constraints**: Peak server RSS below 64 MiB for a 2 GiB upload; no body byte read before every header check passes

**Scale/Scope**: Objects up to 5 GiB; metadata up to 2 KB

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Durability Before Acknowledgement**: PASS. The handler answers 200 only after `Store.Put` returns, which is after the 005 write order and commit. Every failure (including early disconnect) fails before the commit, and 005 removes the temp file.
- **II. Streaming, Bounded Memory**: PASS. The request body is passed to `Store.Put` as a reader; metadata is bounded at 2 KB before the body is read; a 2 GiB e2e test checks peak RSS.
- **III. Standard Library First**: PASS. No new dependencies.
- **IV. Test-First, 100% Coverage**: PASS. Tests first per task; storage failures through `fsx.Faulty`; 100% per `internal/` package.
- **V. Real-Client Compatibility**: PASS. `aws s3api put-object` e2e tests; ETag is the quoted hex MD5; errors use S3 codes and statuses.
- **Scope**: PASS. PutObject is in scope; aws-chunked decoding is deferred to 008 and rejected meanwhile.
- **Development Workflow**: PASS. One branch and PR; README design section updated.

Post-design re-check: PASS.

## Project Structure

```text
specs/007-put-object/  spec.md plan.md research.md data-model.md quickstart.md contracts/http-put-object.md checklists/ tasks.md

internal/storage/
├── storage.go     # PutOptions, Info.ContentType/Metadata, default content type
├── put.go         # Put(..., opts), ErrUnexpectedEOF → ErrIncompleteBody
├── read.go        # record content_type/metadata, info() default
└── *_test.go      # call sites updated; metadata round trip; early EOF

internal/s3api/
├── errors.go      # new APIErrors (R6)
├── storageerr.go  # shared storage → S3 error mapping (replaces bucketOps.apiError)
├── object.go      # ObjectStore, PutObjectOp, header checks
└── *_test.go

internal/app/app.go  # ops.PutObject = s3api.PutObjectOp(store, log)
e2e/objects_test.go  # aws s3api put-object tests, raw early-disconnect test, 2 GiB RSS test
README.md            # 007 design section
```

**Structure Decision**: New file `internal/s3api/object.go` for object operations; the next object features add their Ops there.

## Design Decisions

See [research.md](research.md): R1 CLI capture, R2 `PutOptions` and record fields, R3 early disconnect, R4 check order, R5 wiring and shared error mapping, R6 error table, R7 2 GiB RSS test.

## Complexity Tracking

No constitution violations.
