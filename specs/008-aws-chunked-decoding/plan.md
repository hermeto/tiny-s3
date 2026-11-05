# Implementation Plan: aws-chunked Decoding

**Branch**: `008-aws-chunked-decoding` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/008-aws-chunked-decoding/spec.md`

## Summary

Decode aws-chunked upload bodies in a new `internal/awschunked` package (bounded 4 KiB line buffer, signatures ignored, trailers collected), and verify client checksums (CRC32, CRC32C, CRC64NVME, SHA1, SHA256) from a header or the announced trailer. PutObject replaces 007's 501 for framed uploads with the decoder, uses `X-Amz-Decoded-Content-Length` as the size, and passes a `Verify` callback to `Store.Put` so a mismatch fails before the commit. The real aws CLI is driven through a local TLS proxy to exercise its default chunked framing.

## Technical Context

**Language/Version**: Go 1.27.3 (pinned image)

**Primary Dependencies**: Standard library only (`bufio`, `hash/crc32`, `hash/crc64`, `crypto/sha1`, `crypto/sha256`, `net/http/httputil` in e2e)

**Storage**: Unchanged layout and record

**Testing**: `go test`, `-race`, `go vet`, cover gate, `make e2e`

**Target Platform**: Linux container

**Project Type**: Go server

**Performance Goals**: Decoding adds one 4 KiB buffer and one hash per upload

**Constraints**: Bounded memory independent of chunk size; nothing committed before the checksum is verified

**Scale/Scope**: Any object up to 5 GiB decoded

## Constitution Check

- **I. Durability Before Acknowledgement**: PASS. The checksum is verified in `Verify`, before the file sync and the commit; a mismatch removes the temp file (005 cleanup).
- **II. Streaming, Bounded Memory**: PASS. The decoder streams chunk data and bounds lines at 4 KiB; a 64 MiB single-chunk allocation test.
- **III. Standard Library First**: PASS. No new modules; CRC64NVME uses `hash/crc64` with the NVMe polynomial.
- **IV. Test-First, 100% Coverage**: PASS. New package fully covered; storage `Verify` failure tested; tests first.
- **V. Real-Client Compatibility**: PASS. The aws CLI's default chunked framing is exercised end to end through a TLS proxy; errors use S3 codes.
- **Scope**: PASS. aws-chunked decoding is in scope; SigV4 verification stays out.
- **Development Workflow**: PASS. One PR; README design section.

Post-design re-check: PASS.

## Project Structure

```text
specs/008-aws-chunked-decoding/  spec plan research data-model quickstart contracts/aws-chunked.md tasks

internal/awschunked/   reader.go (+ tests): NewReader, Read, Trailer, ErrMalformed, ErrTruncated
internal/storage/      storage.go (PutOptions.Verify), put.go (call Verify before sync)
internal/s3api/        checksum.go (algorithms, header/trailer selection), object.go (framing, size, verify),
                       errors.go (InvalidRequest), storageerr.go (new mappings)
e2e/chunked_test.go    TLS proxy + aws CLI, signed framing, rejections
README.md              008 design section
```

**Structure Decision**: The framing parser is its own package; checksum selection stays in `s3api` because it reads HTTP headers.

## Design Decisions

See [research.md](research.md): R1 captures, R2 decoder, R3 checksums, R4 `Verify` hook, R5 request order, R6 errors, R7 TLS proxy e2e, R8 memory test.

## Complexity Tracking

No constitution violations.
