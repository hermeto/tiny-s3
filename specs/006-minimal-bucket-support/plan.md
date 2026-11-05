# Implementation Plan: Minimal Bucket Support

**Branch**: `006-minimal-bucket-support` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/006-minimal-bucket-support/spec.md`

## Summary

Add CreateBucket, HeadBucket and ListBuckets, and a gate that answers `NoSuchBucket` for any bucket or object operation on a missing bucket. Buckets are durable records `buckets/<name>.json` in the existing store root, written with the same temp-sync-rename-dirsync order as object records and loaded into an in-memory map by `storage.Open`. `internal/s3api` gains the three operations, the gate (`Ops.CheckBucket`), and the new S3 error codes. `internal/app` opens the store at startup and wires the operations. The e2e suite drives every operation with the real `aws s3api` CLI v2 in a pinned image.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned container image)

**Primary Dependencies**: Go standard library only (`encoding/json`, `encoding/xml`, `sort`/`slices`, `sync`); e2e uses the AWS CLI v2 binary from `amazon/aws-cli:2.31.11` (digest-pinned), not a Go module

**Storage**: Local filesystem under the store root: `tmp/`, `data/`, `meta/` (005) and new `buckets/`

**Testing**: `go test`, `go test -race`, `go vet`, `scripts/cover-gate.sh` (100% per `internal/` package), `make e2e` with `aws s3api`

**Target Platform**: Linux container (production); macOS hosts only run Docker

**Project Type**: Go server

**Performance Goals**: Bucket checks are an in-memory map lookup; creates are rare and pay two file syncs

**Constraints**: A create is acknowledged only after it is durable; one s3d process per data directory; us-east-1 only

**Scale/Scope**: Up to thousands of buckets (S3's default quota is 10,000), all listed in one response

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Durability Before Acknowledgement**: PASS. The principle names PutObject; CreateBucket follows the same order (temp, sync, rename, directory sync) and the renamed record is the commit point (research R2). A failure removes the temp file and answers an S3 error.
- **II. Streaming, Bounded Memory**: PASS. The CreateBucket body is read through a 64 KiB limit; no object body is touched. ListBuckets memory grows with the bucket count, not with any client-controlled size beyond bucket creation.
- **III. Own Storage Engine, Standard Library First**: PASS. Standard library only in production code; the AWS CLI is used only by e2e tests, as a binary in the test image (research R9).
- **IV. Test-First, 100% Coverage**: PASS. Every task writes the failing test first; bucket write-path failures are injected through `fsx.Faulty`; `make cover` covers `storage`, `s3api` and `app`; all tests run under `-race`.
- **V. Real-Client Compatibility**: PASS. Each new operation (CreateBucket, HeadBucket, ListBuckets) and the NoSuchBucket/InvalidBucketName answers have `aws s3api` e2e tests; errors use the S3 XML format and S3's statuses.
- **Scope and Constraints**: PASS. "Minimal bucket operations" is in scope; the bucket index is in memory and rebuilt from durable records at startup, as the constitution requires.
- **Development Workflow**: PASS. One branch, one PR; README design section gains this feature's trade-offs.

Post-design re-check: PASS, no change.

## Project Structure

### Documentation (this feature)

```text
specs/006-minimal-bucket-support/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── storage-buckets.md
│   └── http-buckets.md
├── checklists/requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
internal/storage/
├── storage.go        # + buckets/ in Open, bucket load, bucket errors
├── bucket.go         # Bucket, ValidBucketName, CreateBucket, HeadBucket, ListBuckets, loadBuckets
├── put.go            # writeRecord split into writeTemp (shared with bucket records)
└── bucket*_test.go   # validation, create/head/list, durability faults, load, concurrency

internal/s3api/
├── errors.go         # + InvalidBucketName, NoSuchBucket, BucketAlreadyOwnedByYou, MalformedXML, InvalidLocationConstraint
├── handler.go        # + Ops.CheckBucket gate
├── bucket.go         # BucketStore, BucketOps: CreateBucket, HeadBucket, ListBuckets, CheckBucket
└── *_test.go

internal/app/
├── app.go            # storage.Open at startup; BucketOps wiring
└── app_test.go

e2e/
├── Dockerfile        # aws-cli 2.31.11 + pinned Go toolchain
├── buckets_test.go   # aws s3api tests
├── routing_test.go   # updated for the gate
└── shutdown_test.go  # startProc gains a data-dir variant

Makefile              # e2e builds and runs the e2e image
README.md             # quick start, layout, 006 design trade-offs
```

**Structure Decision**: Bucket logic lives in the existing `internal/storage` and `internal/s3api` packages; no new package.

## Design Decisions

See [research.md](research.md) for rationale and alternatives. Summary:

- **R1** buckets are `buckets/<name>.json` records plus an in-memory map; validated names are safe file names.
- **R2** create: lock, exists check, temp record, sync, rename, dir sync, map insert; rollback on dir-sync failure.
- **R3** one `sync.RWMutex` for the map; create holds it exclusively.
- **R4** `Open` loads every record and fails on any bad one (`ErrCorruptRecord`).
- **R5** `ValidBucketName` implements the S3 general purpose rules.
- **R6** `Ops.CheckBucket` gate after routing, before the NotImplemented fallback; skipped for CreateBucket.
- **R7** HTTP shapes and error mapping per [contracts/http-buckets.md](contracts/http-buckets.md).
- **R8** `app.run` opens the store before listening.
- **R9** `make e2e` runs in `e2e/Dockerfile` (aws-cli image + pinned Go tree).

## Complexity Tracking

No constitution violations. No complexity exception is required.
