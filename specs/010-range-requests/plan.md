# Implementation Plan: Range Requests

**Branch**: `010-range-requests` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/010-range-requests/spec.md`

## Summary

Add single byte-range support to GetObject and HeadObject: a small parser with S3's rules (one range served as 206, past-the-end 416 `InvalidRange`, multiple or malformed ignored), `If-Range` matching, and a section reader through the 009 streaming path.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned image)

**Primary Dependencies**: Standard library only (`io.NewSectionReader`, `http.ParseTime`)

**Storage**: Unchanged; the engine's `Reader` already implements `io.ReaderAt`

**Testing**: `go test`, `-race`, `go vet`, cover gate, `make e2e`

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: Same as full downloads

**Constraints**: Bounded memory for any range; never a short 206

**Scale/Scope**: Ranges within objects up to 5 GiB

## Constitution Check

- **I. Durability**: PASS (read path).
- **II. Streaming, Bounded Memory**: PASS. Section reader through the fixed 32 KiB buffer; 1 GiB range RSS check.
- **III. Standard Library First**: PASS.
- **IV. Test-First, 100% Coverage**: PASS. Parser table tests, handler tests, read faults through `fsx.Faulty`.
- **V. Real-Client Compatibility**: PASS. `aws s3api get-object --range` e2e; S3 codes and headers.
- **Scope / Workflow**: PASS.

Post-design re-check: PASS.

## Project Structure

```text
specs/010-range-requests/  spec plan research data-model quickstart contracts/http-range.md tasks

internal/s3api/
├── rangereq.go       # parseRange, ifRangeMatches
├── object.go         # GET/HEAD apply the range
├── errors.go         # InvalidRange
└── *_test.go
e2e/range_test.go     # CLI ranges, If-Range, 1 GiB range RSS (in the 2 GiB test)
README.md             # 010 design section
```

## Design Decisions

See [research.md](research.md): R1 parsing, R2 If-Range, R3 order, R4 errors, R5 proofs.

## Complexity Tracking

No constitution violations.
