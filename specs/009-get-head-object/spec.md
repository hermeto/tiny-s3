# Feature Specification: GetObject and HeadObject

**Feature Branch**: `009-get-head-object`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Read objects back from tiny-s3. A client downloads an object as a stream and gets the same metadata that was stored at upload; a client can also fetch the metadata alone. Acceptance criteria: aws s3api get-object writes a file byte-identical to the uploaded one. The response carries ETag, Content-Length, Content-Type, Last-Modified (RFC 1123 GMT), Accept-Ranges: bytes and the x-amz-meta-* headers. aws s3api head-object returns the same headers with no body. A missing key returns 404 NoSuchKey (HEAD with no body). Conditional requests work: If-None-Match with the current ETag returns 304, If-Match with a different ETag returns 412 PreconditionFailed, and If-Modified-Since in the future returns 304. Downloading a large object uses flat server memory. A GET that started before a concurrent overwrite or delete finishes with the complete original bytes."

## Clarifications

### Session 2026-11-04

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: In which order are conditional headers evaluated when several are sent? → A: The HTTP order S3 follows: `If-Match`, or `If-Unmodified-Since` only when `If-Match` is absent (412 on failure); then `If-None-Match`, or `If-Modified-Since` only when `If-None-Match` is absent (304 on failure). So `If-Match` true with `If-Unmodified-Since` false returns the object, and `If-None-Match` false with `If-Modified-Since` true returns 304.
- Q: At what precision are dates compared? → A: Whole seconds: the last-modified time is truncated to the second, as sent in `Last-Modified`.
- Q: What does a 304 response carry? → A: Status 304, `ETag`, `Last-Modified` and the request ID; no body and no `Content-Length`/`Content-Type`.
- Q: What does a conditional request for a missing key return? → A: 404 `NoSuchKey`; existence is checked before conditions.
- Q: Do conditional headers apply to metadata (HEAD) requests? → A: Yes, with the same results; a 412 on HEAD has no body.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Download an object (Priority: P1)

As an S3 client user, I download an object I uploaded and get exactly the same bytes, with its ETag, size, content type, last-modified date and my custom metadata.

**Why this priority**: Reading back what was stored is the purpose of an object store.

**Independent Test**: Upload a file with a content type and metadata, download it with the S3 command-line client, compare the bytes and check every metadata field.

**Acceptance Scenarios**:

1. **Given** an object uploaded from a file, **When** the client downloads it, **Then** the written file is byte-identical to the original.
2. **Given** an object uploaded with content type `text/plain` and metadata `owner=alice`, **When** the client downloads it, **Then** the response carries the ETag, the size, `text/plain`, the last-modified date in RFC 1123 GMT form, `Accept-Ranges: bytes` and `x-amz-meta-owner: alice`.
3. **Given** a key that does not exist, **When** the client downloads it, **Then** the request fails with 404 `NoSuchKey`.
4. **Given** a bucket that does not exist, **When** the client downloads from it, **Then** the request fails with 404 `NoSuchBucket` (feature 006).

---

### User Story 2 - Fetch metadata only (Priority: P1)

As an S3 client user, I fetch an object's metadata without its body, to check it exists or inspect it cheaply.

**Why this priority**: Clients and tools check objects before downloading; it must match GET exactly.

**Independent Test**: Run head-object on an existing and a missing key and compare the headers with those of a download.

**Acceptance Scenarios**:

1. **Given** an existing object, **When** the client fetches its metadata, **Then** the response has the same headers as a download and no body.
2. **Given** a missing key, **When** the client fetches its metadata, **Then** the response is 404 with no body.

---

### User Story 3 - Conditional requests (Priority: P2)

As an S3 client user, I can make a download or metadata request conditional on the object's ETag or modification date, to avoid transferring data I already have or to detect changes.

**Why this priority**: Caches and sync tools rely on conditional requests; they save bandwidth but are not needed for basic use.

**Independent Test**: Send downloads with each conditional header against an existing object and check status codes.

**Acceptance Scenarios**:

1. **Given** an object with ETag E, **When** the client sends `If-None-Match: E`, **Then** the response is 304 Not Modified without a body.
2. **Given** an object with ETag E, **When** the client sends `If-Match` with a different ETag, **Then** the response is 412 `PreconditionFailed`.
3. **Given** an object, **When** the client sends `If-Modified-Since` with a date in the future, **Then** the response is 304.
4. **Given** an object modified after date D, **When** the client sends `If-Unmodified-Since: D`, **Then** the response is 412 `PreconditionFailed`.
5. **Given** matching conditions (`If-Match: E`, `If-None-Match` with another ETag, `If-Modified-Since` in the past), **When** the client sends them, **Then** the object is returned normally.

---

### User Story 4 - Large and concurrent downloads (Priority: P2)

As an operator, downloads of any size use a fixed amount of server memory, and a download that already started is not disturbed by a concurrent overwrite or delete of the same key.

**Why this priority**: Memory safety and read consistency under load; they build on stories 1 and 2.

**Independent Test**: Download a 2 GiB object while watching the server's peak memory; start a download, overwrite and delete the key, and finish the download.

**Acceptance Scenarios**:

1. **Given** a 2 GiB object, **When** it is downloaded, **Then** the server's peak memory stays under a fixed bound independent of the object size.
2. **Given** a download in progress, **When** the key is overwritten and then deleted, **Then** the download completes with exactly the original bytes.

### Edge Cases

- Zero-byte objects download as an empty body with `Content-Length: 0`.
- Objects stored before feature 007 (no content type) report `binary/octet-stream`.
- A storage read failure after the response has started aborts the connection, so the client sees a failed transfer rather than a short success.
- ETags in conditional headers may be quoted or unquoted, weak (`W/`) or `*`; a list of ETags matches if any one matches.
- An unparseable date in `If-Modified-Since` or `If-Unmodified-Since` is ignored.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Users MUST be able to download an existing object; the body MUST be the stored bytes, streamed.
- **FR-002**: Download and metadata responses MUST carry `ETag` (quoted hex MD5), `Content-Length`, `Content-Type`, `Last-Modified` (RFC 1123, GMT), `Accept-Ranges: bytes` and one `x-amz-meta-<name>` header per stored metadata entry.
- **FR-003**: A metadata request MUST return the same status and headers as the matching download, without a body.
- **FR-004**: A missing key MUST answer 404 `NoSuchKey` (no body for metadata requests).
- **FR-005**: Conditional headers MUST be evaluated as S3 does: `If-Match` and `If-Unmodified-Since` failures answer 412 `PreconditionFailed`; `If-None-Match` and `If-Modified-Since` failures answer 304 Not Modified with no body.
- **FR-006**: Server memory per download MUST NOT grow with object size.
- **FR-007**: A download that started MUST complete with the version it started on, regardless of concurrent overwrites and deletes.
- **FR-008**: Errors MUST use the S3 XML error format; a storage failure before the response starts answers 500 `InternalError`, and after it starts aborts the connection.
- **FR-009**: Both operations MUST be exercised end to end by the real S3 command-line client (constitution Principle V).
- **FR-010**: Conditional headers MUST be evaluated in the order given in Clarifications, comparing dates in whole seconds, for both downloads and metadata requests, after the existence check.
- **FR-011**: A 304 response MUST carry `ETag` and `Last-Modified` and no body.

### Key Entities

- **Object** (from 005/007): bucket, key, size, MD5 (ETag), last-modified, content type, user metadata, body.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of downloaded files are byte-identical to the uploaded files, for sizes from 0 bytes to 10 MiB with the command-line client, and 2 GiB with a streaming client.
- **SC-002**: A 2 GiB download keeps the server's peak memory below 64 MiB.
- **SC-003**: Every conditional scenario in User Story 3 returns the specified status.
- **SC-004**: A download in progress during an overwrite and a delete returns 100% of the original bytes.

## Assumptions

- Range requests are feature 010; this feature always returns the whole object (it already advertises `Accept-Ranges: bytes`).
- `response-*` query parameters (overriding response headers) are out of scope.
- Checksums supplied at upload are not stored (feature 008), so no checksum headers are returned.
- No versioning: there is one current version per key.
- DeleteObject over HTTP is feature 011. Until then, a delete during a download is proved with the storage engine's delete at the handler level; feature 011 adds the HTTP delete to the end-to-end test.
