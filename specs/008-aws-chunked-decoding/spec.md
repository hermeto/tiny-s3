# Feature Specification: aws-chunked Decoding

**Feature Branch**: `008-aws-chunked-decoding`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Accept uploads from aws CLI v2.31 and current AWS SDKs with their default settings. These clients send PutObject bodies with Content-Encoding aws-chunked, a decoded-length header and a trailing checksum. tiny-s3 must store exactly the decoded object bytes and reject uploads whose trailing checksum does not match. Acceptance criteria: With no special environment variables, aws s3api put-object stores bytes identical to the source file and returns the MD5 ETag. Uploads with x-amz-content-sha256 STREAMING-UNSIGNED-PAYLOAD-TRAILER and with STREAMING-AWS4-HMAC-SHA256-PAYLOAD (chunk signatures present but not verified) both work. A mismatched trailing checksum returns 400 BadDigest and stores nothing. A malformed chunk stream returns 400 IncompleteBody or InvalidRequest. Decoding streams with bounded memory."

## Clarifications

### Session 2026-11-04

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: What is the maximum length of a chunk-header or trailer line? → A: 4096 bytes including the line ending; a longer line fails with 400 `InvalidRequest`.
- Q: Does a successful upload echo the verified checksum back in a response header? → A: No; the response stays `ETag` plus request ID (feature 007, FR-014).
- Q: Is the client's checksum stored with the object for later reads? → A: No; it is verified and discarded. Returning checksums on GetObject is out of scope.
- Q: Which trailers does the server read? → A: Only the checksum trailer named in `x-amz-trailer`; other trailers and the trailer signature are accepted and ignored. A named checksum trailer that never arrives fails with 400 `InvalidRequest`.
- Q: Does the 5 GiB object limit apply to chunked uploads? → A: Yes, to the declared decoded length, with 400 `EntityTooLarge` before the body is read.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Upload with default client settings (Priority: P1)

As an S3 client user, I upload with the S3 command-line client or an AWS SDK without changing any setting, and the server stores exactly my file's bytes and returns the MD5 ETag, whichever upload framing the client chose.

**Why this priority**: Current clients frame uploads in chunks with a trailing checksum by default (over HTTPS in particular); a server that cannot read that framing fails every default upload.

**Independent Test**: Upload files of several sizes with the command-line client in its default configuration, through an endpoint where it uses chunked framing, and compare the stored bytes and ETag with the source.

**Acceptance Scenarios**:

1. **Given** a client that sends the body in chunked framing with a trailing checksum and no signatures, **When** it uploads a file, **Then** the request succeeds with the file's MD5 ETag and the stored object is byte-identical to the file.
2. **Given** a client that sends the body in chunked framing with a signature on each chunk, **When** it uploads a file, **Then** the request succeeds the same way; the signatures are accepted without verification.
3. **Given** a client that sends a plain body with its checksum in a header (no chunked framing), **When** it uploads a file, **Then** the request succeeds as before.
4. **Given** an empty file, **When** it is uploaded in chunked framing, **Then** the request succeeds with the ETag of empty content.

---

### User Story 2 - Reject uploads whose checksum does not match (Priority: P1)

As an S3 client user, if the bytes the server received do not match the checksum my client computed, the upload fails and nothing is stored.

**Why this priority**: The checksum is the client's proof of integrity; ignoring it would let corruption in transit go unnoticed.

**Independent Test**: Send chunked uploads whose trailing checksum is wrong, and plain uploads with a wrong checksum header, for each supported algorithm.

**Acceptance Scenarios**:

1. **Given** a chunked upload whose trailing checksum does not match the decoded bytes, **When** it completes, **Then** it fails with 400 `BadDigest` and nothing is stored (a previous object under the key is unchanged).
2. **Given** a plain upload whose checksum header does not match the body, **When** it completes, **Then** it fails with 400 `BadDigest` and nothing is stored.
3. **Given** a checksum value that is not valid for its algorithm (bad encoding or length), **When** the upload arrives, **Then** it fails with 400 `InvalidRequest`.

---

### User Story 3 - Reject malformed chunk streams (Priority: P2)

As an operator, a client that sends a broken chunk stream gets a clear error and nothing is stored.

**Why this priority**: Robustness against buggy or hostile clients; it matters less than accepting good uploads.

**Independent Test**: Send chunk streams with bad size lines, missing line endings, truncation and decoded lengths that disagree with the declared one.

**Acceptance Scenarios**:

1. **Given** a chunk stream that ends before its final chunk, **When** it arrives, **Then** the upload fails with 400 `IncompleteBody` and nothing is stored.
2. **Given** a chunk stream with an unparseable chunk header or missing line ending, **When** it arrives, **Then** the upload fails with 400 `InvalidRequest` and nothing is stored.
3. **Given** a chunk stream whose decoded bytes are fewer or more than the declared decoded length, **When** it arrives, **Then** the upload fails with 400 `IncompleteBody` and nothing is stored.
4. **Given** a chunked upload without a declared decoded length, **When** it arrives, **Then** it fails with 411 `MissingContentLength` before the body is read.

---

### User Story 4 - Bounded memory while decoding (Priority: P2)

As an operator, chunked uploads of any size, and chunks of any declared size, use a fixed amount of server memory.

**Why this priority**: Chunk sizes are client-controlled; buffering a chunk would reopen the memory problem 005 and 007 closed.

**Independent Test**: Upload a large object in one huge chunk and in many small chunks, and check the memory used while decoding.

**Acceptance Scenarios**:

1. **Given** a 64 MiB object sent as a single chunk, **When** it is decoded, **Then** decoding memory stays under a fixed bound independent of the chunk size.
2. **Given** a chunk header or trailer line longer than the allowed line length, **When** it arrives, **Then** the upload fails with 400 `InvalidRequest` instead of buffering the line.

### Edge Cases

- `Content-Encoding` lists aws-chunked together with another encoding (`aws-chunked,gzip`): the framing is removed and the stored bytes are the payload the client produced.
- The chunked framing may arrive inside an HTTP chunked transfer (no `Content-Length`): the declared decoded length is the object size.
- A trailer announced by the client but missing from the stream fails with 400 `InvalidRequest`.
- More than one checksum header or trailer fails with 400 `InvalidRequest`.
- Checksum headers for algorithms the server does not know are ignored.
- A trailer signature (`x-amz-trailer-signature`) is accepted without verification.
- A client that disconnects mid-stream gets `IncompleteBody`, as in 007.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The server MUST accept PutObject bodies in aws-chunked framing (signaled by `Content-Encoding` containing `aws-chunked` or a `STREAMING-` content hash) and store exactly the decoded bytes.
- **FR-002**: Both unsigned framing with trailers and signed framing (chunk signatures and, when present, a trailer signature) MUST be accepted; signatures are not verified.
- **FR-003**: The object size MUST be the declared decoded length; a chunked upload without it MUST fail with 411 `MissingContentLength`, and decoded bytes that differ from it MUST fail with 400 `IncompleteBody`.
- **FR-004**: The server MUST verify the object's checksum when the client supplies one, as a trailer or as a header, for CRC32, CRC32C, CRC64NVME, SHA1 and SHA256; a mismatch MUST fail with 400 `BadDigest` and store nothing.
- **FR-005**: A malformed chunk stream MUST fail with 400 `InvalidRequest`; a truncated one with 400 `IncompleteBody`; nothing is stored in either case.
- **FR-006**: Decoding MUST use bounded memory, independent of object and chunk sizes, with a limit on chunk-header and trailer line length.
- **FR-007**: The ETag MUST remain the quoted hex MD5 of the decoded bytes.
- **FR-008**: Uploads with the default settings of the S3 command-line client MUST be exercised end to end, including its chunked framing (constitution Principle V).
- **FR-009**: Chunk-header and trailer lines MUST be at most 4096 bytes; longer lines fail with 400 `InvalidRequest`.
- **FR-010**: The verified checksum MUST NOT be echoed or stored; the success response stays as in feature 007.
- **FR-011**: A declared decoded length above 5 GiB MUST fail with 400 `EntityTooLarge` before the body is read.

### Key Entities

- **Chunk stream**: a sequence of size-prefixed chunks, optionally signed, ending with a zero-size chunk and optional trailers.
- **Checksum**: an algorithm name and a base64 value supplied by the client, as a header or a trailer.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of default-setting uploads from the command-line client are stored byte-identical to the source, for sizes from 0 bytes to at least 10 MiB.
- **SC-002**: 100% of uploads with a wrong checksum, in each of the five algorithms, are rejected and leave no object.
- **SC-003**: Decoding a 64 MiB single-chunk upload allocates less than 4 MiB.
- **SC-004**: Every malformed-stream case in the acceptance scenarios is rejected with the specified status and code.

## Assumptions

- The command-line client uses chunked framing with a trailing CRC64NVME checksum over HTTPS and plain bodies with a checksum header over plain HTTP (verified with captures of CLI 2.31.11). tiny-s3 serves plain HTTP; tests reach the chunked framing by putting a TLS-terminating proxy in front of the server.
- SigV4 verification stays out of scope (constitution), including chunk and trailer signatures and the hex `X-Amz-Content-SHA256` of plain bodies.
- Only PutObject accepts bodies in this project; multipart upload is out of scope.
