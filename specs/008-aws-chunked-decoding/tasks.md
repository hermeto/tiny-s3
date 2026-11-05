# Tasks: aws-chunked Decoding

**Input**: Design documents from `/specs/008-aws-chunked-decoding/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: Required (constitution IV and V); every test task is written first and observed failing.

## Phase 1: Setup

- [ ] T001 Create `internal/awschunked/reader.go` with the package doc comment, `ErrMalformed`, `ErrTruncated`, the `Reader` type and stub `NewReader`, `Read` and `Trailer` per `contracts/aws-chunked.md` (stubs return errors, so the package compiles and the tests fail)

---

## Phase 2: Foundational (Blocking Prerequisites)

- [ ] T002 [P] Write failing tests in `internal/awschunked/reader_test.go`: the captured CLI stream (`b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:jSnVw/bqjr4=\r\n\r\n`) decodes to `hello world` with that trailer; several chunks; signed chunks (`;chunk-signature=<64 hex>`) with and without trailers and with `x-amz-trailer-signature`; an empty object (`0\r\n\r\n`); upper-case hex sizes; `Trailer` before EOF returns false; trailer names are case-insensitive; reads with 1-byte buffers
- [ ] T003 [P] Write failing malformed/truncated tests in `internal/awschunked/reader_test.go`: bad hex, empty size, size over 63 bits, missing CRLF after data, bare LF, line of 4097 bytes ("at most 4096 bytes including CRLF"), trailer without `:`, 17 trailers ("at most 16"), bytes after the final CRLF → `ErrMalformed`; EOF or `io.ErrUnexpectedEOF` (what net/http returns for a cut HTTP chunked body) inside a header, data, trailer or before the final CRLF → `ErrTruncated`; any other underlying read error passes through unchanged
- [ ] T004 [P] Write a failing bounded-memory test in `internal/awschunked/memory_test.go`: a lazily generated 64 MiB single-chunk stream decodes through `io.CopyBuffer` with `TotalAlloc` growth below 4 MiB
- [ ] T005 Implement the decoder in `internal/awschunked/reader.go` (research R2)
- [ ] T006 [P] Write a failing test in `internal/storage/put_test.go`: `PutOptions.Verify` is called once after the body is read and before the commit; its error fails `Put` (errors.Is the returned error), leaves the previous object unchanged and `tmp/`/`data/` without new files; a nil `Verify` changes nothing
- [ ] T007 Add `Verify` to `PutOptions` in `internal/storage/storage.go` and call it in `internal/storage/put.go` after the MD5 check, before the sync
- [ ] T008 [P] Write failing tests in `internal/s3api/checksum_test.go`: digests of `hello world` for the five algorithms (CRC64NVME `jSnVw/bqjr4=`); header selection (one header → expected value; two headers → `InvalidRequest`; unknown `x-amz-checksum-foo` ignored; bad base64 or wrong length → `InvalidRequest`); trailer selection from `X-Amz-Trailer` (a checksum trailer plus a header → `InvalidRequest`; a non-checksum trailer name → no checksum); `ErrInvalidRequest` APIError value in `internal/s3api/errors_test.go`
- [ ] T009 Implement `internal/s3api/checksum.go` and add `ErrInvalidRequest` to `internal/s3api/errors.go`

**Checkpoint**: decoder, Verify hook and checksum selection green and at 100%.

---

## Phase 3: User Story 1 - Upload with default client settings (Priority: P1) 🎯 MVP

**Goal**: Framed and plain uploads with checksums are stored as their decoded bytes.

**Independent Test**: Default aws CLI upload through the TLS proxy returns the file's MD5 ETag.

- [ ] T010 [P] [US1] Write failing handler tests in `internal/s3api/object_test.go`: a framed upload (unsigned with trailer, signed, signed with trailer, `aws-chunked,gzip`, HTTP `Transfer-Encoding: chunked` with `ContentLength == -1`) reaches the store with the decoded bytes and `size` = `X-Amz-Decoded-Content-Length`, and answers 200 with the MD5 ETag; a plain upload with a correct `x-amz-checksum-crc64nvme` header succeeds; replace 007's "aws-chunked → 501" cases
- [ ] T011 [US1] Implement framing selection, decoded length and checksum verification in `internal/s3api/object.go` (research R5) using `awschunked` and `checksum.go`
- [ ] T012 [US1] Write `e2e/chunked_test.go`: a TLS reverse proxy helper (`httptest.NewTLSServer` + `httputil.NewSingleHostReverseProxy`, certificate written for `AWS_CA_BUNDLE`), and aws CLI uploads through it of 0 bytes, 11 bytes and 10 MiB of random data with default settings, and with `--checksum-algorithm` CRC32, CRC32C, SHA1, SHA256; each ETag equals the file's MD5; plus a default plain-HTTP upload (checksum header)

**Checkpoint**: The CLI's default uploads work over HTTPS and HTTP.

---

## Phase 4: User Story 2 - Reject checksum mismatches (Priority: P1)

- [ ] T013 [P] [US2] Write failing handler tests in `internal/s3api/object_test.go` with the real store: a wrong trailer for each algorithm → 400 `BadDigest`, nothing stored, a previous object unchanged; a wrong checksum header on a plain upload → `BadDigest`; an invalid header value → `InvalidRequest` before the body is read; an invalid trailer value or an announced trailer that never arrives → `InvalidRequest`
- [ ] T014 [US2] Add the checksum mismatch and invalid-checksum errors to the shared mapping in `internal/s3api/storageerr.go` (and anything T013 still finds missing in `internal/s3api/object.go`)
- [ ] T015 [US2] Add e2e tests to `e2e/chunked_test.go`: `--checksum-crc32` with a wrong value over plain HTTP → `(BadDigest)`; a raw framed upload with a wrong CRC64NVME trailer → 400 `BadDigest`

---

## Phase 5: User Story 3 - Reject malformed chunk streams (Priority: P2)

- [ ] T016 [P] [US3] Write failing handler tests in `internal/s3api/object_test.go` (real store): truncated stream → `IncompleteBody`; bad chunk header, long line → `InvalidRequest`; decoded bytes fewer or more than declared → `IncompleteBody`; no `X-Amz-Decoded-Content-Length` → 411 and not a number → `InvalidArgument`, both before the body is read; decoded length over 5 GiB → `EntityTooLarge` before the body is read; nothing stored in every case
- [ ] T017 [US3] Map `awschunked.ErrMalformed` → `InvalidRequest` and `ErrTruncated` → `IncompleteBody` in `internal/s3api/storageerr.go`; fix whatever T016 still finds in `internal/s3api/object.go`
- [ ] T018 [US3] Add raw e2e tests to `e2e/chunked_test.go`: a signed (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`) upload with fake signatures succeeds with the MD5 ETag; a truncated stream → 400 `IncompleteBody`; a malformed chunk header → 400 `InvalidRequest`

---

## Phase 6: User Story 4 - Bounded memory while decoding (Priority: P2)

Covered by T004 (64 MiB single chunk) and T003 (line limit). No extra task.

---

## Phase 7: Polish

- [ ] T019 Add the `### 008 aws-chunked decoding` design section to `README.md` (research R1–R8) and update the 007 bullets that said aws-chunked and checksums were not handled; record that `Content-Encoding` is not stored, so an `aws-chunked,gzip` upload reads back as the gzip payload without its encoding header
- [ ] T020 Run `make vet test race cover e2e`; 100% for every `internal/` package
- [ ] T021 Run `quickstart.md` against `docker compose up -d --build`, then `docker compose down -v`

## Dependencies & Execution Order

- T001 → T002–T005; T006 → T007; T008 → T009. Phase 2 blocks the stories.
- US1 (T010–T012) is the MVP; US2 and US3 extend `object.go` and `storageerr.go` after T011.

## Parallel Opportunities

- T002/T003/T004 (awschunked), T006 (storage) and T008 (s3api) are independent packages.

## Implementation Strategy

Decoder and checksum building blocks first, then wire them into PutObject (MVP with the real CLI), then rejections.

## Analysis Remediation (2026-11-04)

`/speckit-analyze` findings, fixed with the recommended option:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| I1 | MEDIUM | The spec's edge case says a client disconnecting mid-stream gets `IncompleteBody`, but the contract let every underlying error pass through, and net/http reports a cut chunked body as `io.ErrUnexpectedEOF`. | T003 maps `io.ErrUnexpectedEOF` to `ErrTruncated` too; the contract is read that way. |
| I2 | MEDIUM | The `aws-chunked,gzip` edge case stores the gzip payload, but 007 does not store `Content-Encoding`, so the object reads back without its encoding. | Accepted; T019 records it in the README. |
| C1 | LOW | User Story 4 has no tasks of its own. | Covered by T003 (line limit) and T004 (64 MiB single chunk); stated in Phase 6. |

No constitution conflicts. Coverage: FR-001–FR-011 and SC-001–SC-004 each map to at least one task (100%).
