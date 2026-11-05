# Implementation Plan: Ordered Index and ListObjectsV2

**Branch**: `012-list-objects-v2` | **Date**: 2026-11-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/012-list-objects-v2/spec.md`

## Summary

Add an in-memory ordered index to the storage engine (sorted keys per bucket, rebuilt from `meta/` at startup, updated under the key lock exactly when a record file changes) and serve ListObjectsV2 from it: prefix, delimiter with common prefixes, start-after, max-keys and opaque continuation tokens that encode a position, plus URL encoding.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned image)

**Primary Dependencies**: Standard library only (`slices`, `encoding/base64`, `encoding/xml`)

**Storage**: Unchanged on disk; new in-memory index

**Testing**: `go test`, `-race`, `go vet`, cover gate, `make e2e`

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: A 1,000-key page from 10,000 keys within 100 ms (SC-004)

**Constraints**: No disk reads per listing; listing consistent with Head at all times

**Scale/Scope**: Tens of thousands of keys per bucket comfortably; the sorted slice's O(n) inserts are the known limit

## Constitution Check

- **I. Durability Before Acknowledgement**: PASS. The write path is unchanged; the index follows the record files and adds no acknowledgement of its own.
- **II. Streaming, Bounded Memory**: PASS. Listing memory is bounded by the page (≤ 1000 entries); index memory grows with the number of objects, not their size, as the constitution's in-memory index implies.
- **III. Standard Library First**: PASS.
- **IV. Test-First, 100% Coverage**: PASS. Index and List unit tests, rebuild with corrupt and unreadable records through `fsx.Faulty`, consistency tests under `-race`.
- **V. Real-Client Compatibility**: PASS. `aws s3api list-objects-v2` and `aws s3 ls` e2e.
- **Scope and Constraints**: PASS. "The index is held in memory and rebuilt from the durable metadata records at startup."
- **Development Workflow**: PASS.

Post-design re-check: PASS.

## Project Structure

```text
specs/012-list-objects-v2/  spec plan research data-model quickstart contracts/http-list-objects-v2.md tasks

internal/storage/
├── index.go        # objectIndex, put/remove/list
├── list.go         # Store.List, ListOptions, ListResult, ObjectSummary
├── storage.go      # Open rebuilds the index
├── put.go, delete.go  # index updates at the record changes
└── *_test.go
internal/s3api/
├── list.go         # ListObjectsV2Op, token codec, URL encoding, XML
├── errors.go       # InvalidArgument messages reused
└── *_test.go
internal/app/app.go # wire ListObjectsV2
e2e/list_test.go    # CLI listing, aws s3 ls, 2,500-key paging under writes
README.md           # 012 design section
```

## Design Decisions

See [research.md](research.md): R1 structure, R2 mirroring records, R3 rebuild, R4 algorithm and tokens, R5 HTTP and URL encoding.

## Complexity Tracking

No constitution violations.
