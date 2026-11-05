# Feature Specification: PutObject

**Feature Branch**: `007-put-object`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "PutObject over HTTP for tiny-s3. A client uploads an object of any size to an existing bucket and gets back the S3 ETag once the object is durable. Acceptance criteria: aws s3api put-object --bucket demo --key a/b.txt --body file returns 200 with an ETag equal to the quoted hex MD5 of the file. Content-Type and x-amz-meta-* headers are stored and returned later. A Content-MD5 header that does not match the body returns 400 BadDigest. A body shorter than Content-Length (client disconnect) returns 400 IncompleteBody and stores nothing. A missing Content-Length without aws-chunked encoding returns 411 MissingContentLength. Keys longer than 1024 bytes return 400 KeyTooLongError. A 2 GiB upload completes with flat server memory. Uploading to a missing bucket returns 404 NoSuchBucket."

## Clarifications

### Session 2026-11-04

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: What does this feature answer to an aws-chunked upload, before feature 008 decodes it? → A: 501 `NotImplemented` when `Content-Encoding` names `aws-chunked` or `x-amz-content-sha256` starts with `STREAMING-`, before any body byte is read, so framing bytes are never stored as object data.
- Q: How is an HTTP `Transfer-Encoding: chunked` upload without `Content-Length` treated? → A: As a missing length: 411 `MissingContentLength`, as S3 does.
- Q: How is the 2 KB user-metadata limit measured, and how are values stored? → A: The sum of the bytes of every metadata name (without the `x-amz-meta-` prefix) and value; above 2048 bytes fails with 400 `MetadataTooLarge`. Values are stored exactly as received; repeated headers with the same name are joined with `,`.
- Q: Which headers does a successful upload return? → A: `ETag` and the request ID only; no version ID or checksum echo.
- Q: How do objects written before this feature (no content type or metadata in their record) behave? → A: The record keeps format version 1 with two optional fields; a record without them reads as content type `binary/octet-stream` and no metadata, so no migration is needed.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Upload an object and get its ETag (Priority: P1)

As an S3 client user, I upload a file to an existing bucket with the standard S3 command-line client and get back the object's ETag, knowing that once I have it the object is safely stored.

**Why this priority**: Uploading is the first thing anyone does with an object store; every later read, list and delete depends on it.

**Independent Test**: Create a bucket, upload a file with the S3 command-line client, and compare the returned ETag with the file's MD5.

**Acceptance Scenarios**:

1. **Given** bucket `demo` exists, **When** the client uploads a file as key `a/b.txt`, **Then** the request succeeds with status 200 and an ETag equal to the quoted lowercase hex MD5 of the file.
2. **Given** an object already exists under a key, **When** the client uploads a different file to the same key, **Then** the request succeeds and the new ETag matches the new file; the key now holds the new content.
3. **Given** bucket `missing` does not exist, **When** the client uploads to it, **Then** the request fails with status 404 and error code `NoSuchBucket`.
4. **Given** a zero-byte file, **When** the client uploads it, **Then** the request succeeds with the ETag of empty content.

---

### User Story 2 - Keep the object's content type and user metadata (Priority: P1)

As an S3 client user, I set a content type and custom `x-amz-meta-*` metadata when uploading, and they are kept with the object so they can be returned when the object is read.

**Why this priority**: Clients rely on content type and metadata for serving and tagging objects; losing them silently breaks applications.

**Independent Test**: Upload with a content type and two metadata entries, then inspect the stored object's metadata.

**Acceptance Scenarios**:

1. **Given** an upload with `Content-Type: text/plain` and `x-amz-meta-owner: alice`, **When** it succeeds, **Then** the stored object records that content type and the metadata entry `owner: alice`.
2. **Given** an upload without a content type, **When** it succeeds, **Then** the stored content type is `binary/octet-stream`, as S3 does.
3. **Given** an overwrite without metadata, **When** it succeeds, **Then** the previous object's metadata is gone, not merged.

---

### User Story 3 - Reject corrupt or incomplete uploads (Priority: P1)

As an S3 client user, I am told when my upload arrived damaged or incomplete, and nothing is stored, so I never read back a corrupted object.

**Why this priority**: Accepting damaged data silently is the worst failure an object store can have.

**Independent Test**: Send uploads with a wrong Content-MD5, a body shorter than declared, and no declared length; check each error and that no object appears.

**Acceptance Scenarios**:

1. **Given** a `Content-MD5` header that does not match the body, **When** the upload completes, **Then** the request fails with 400 `BadDigest` and nothing is stored.
2. **Given** a client that declares N bytes and disconnects before sending them all, **When** the server notices, **Then** the upload fails with 400 `IncompleteBody` and nothing is stored (any previous object under the key is unchanged).
3. **Given** an upload without `Content-Length` and without aws-chunked encoding, **When** it arrives, **Then** it fails with 411 `MissingContentLength` before any body is read.
4. **Given** a key longer than 1024 bytes, **When** it is uploaded, **Then** the request fails with 400 `KeyTooLongError`.
5. **Given** a `Content-MD5` header that is not a base64-encoded 16-byte value, **When** the upload arrives, **Then** it fails with 400 `InvalidDigest`.

---

### User Story 4 - Upload large objects with flat memory (Priority: P2)

As an operator, I can let clients upload multi-gigabyte objects without the server's memory growing with object size.

**Why this priority**: Object size is client-controlled; memory growth would let one upload take the server down.

**Independent Test**: Upload a 2 GiB object and watch the server's peak memory.

**Acceptance Scenarios**:

1. **Given** a 2 GiB body, **When** it is uploaded, **Then** the request succeeds with the correct ETag and the server's peak memory stays under a fixed bound independent of the object size.
2. **Given** a declared length above 5 GiB, **When** the upload arrives, **Then** it fails with 400 `EntityTooLarge` before the body is read.

### Edge Cases

- Keys with `/`, spaces, `+`, percent-encoded characters and non-ASCII UTF-8 are stored under exactly the key the client sent.
- An upload naming an invalid bucket answers `InvalidBucketName`, not `NoSuchBucket` (feature 006).
- An upload whose body is longer than its declared length cannot happen over HTTP/1.1 (the extra bytes are the next request); the declared length is authoritative.
- Metadata header names are case-insensitive; they are stored in lowercase.
- User metadata larger than 2 KB in total is rejected with 400 `MetadataTooLarge`.
- An aws-chunked upload (Content-Encoding `aws-chunked`, or a `STREAMING-*` content hash) is rejected with 501 `NotImplemented` before its body is read, rather than stored with its framing; decoding comes with feature 008.
- An HTTP `Transfer-Encoding: chunked` upload without `Content-Length` fails with 411 `MissingContentLength`.
- Repeated metadata headers with the same name are stored as one value joined with `,`.
- A storage failure during the upload answers 500 `InternalError` and stores nothing.
- Two uploads to the same key at the same time end with exactly one of them stored.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Users MUST be able to upload an object to an existing bucket; success returns status 200 and an `ETag` header equal to the quoted lowercase hex MD5 of the stored bytes.
- **FR-002**: The server MUST answer success only after the object is durable (constitution Principle I).
- **FR-003**: The upload body MUST be streamed; server memory per upload MUST NOT grow with object size.
- **FR-004**: The object's content type (default `binary/octet-stream`) and every `x-amz-meta-*` header MUST be stored with the object, replacing any previous object's values.
- **FR-005**: A `Content-MD5` that does not match the body MUST fail with 400 `BadDigest`; a malformed `Content-MD5` MUST fail with 400 `InvalidDigest`; nothing is stored in either case.
- **FR-006**: A body that ends before its declared length MUST fail with 400 `IncompleteBody` and store nothing.
- **FR-007**: An upload without `Content-Length` and without aws-chunked encoding MUST fail with 411 `MissingContentLength`.
- **FR-008**: A key longer than 1024 bytes MUST fail with 400 `KeyTooLongError`; a declared length above 5 GiB MUST fail with 400 `EntityTooLarge`; both before the body is read.
- **FR-009**: An upload to a missing bucket MUST fail with 404 `NoSuchBucket` (feature 006 gate).
- **FR-010**: User metadata whose names (without prefix) and values sum to more than 2048 bytes MUST fail with 400 `MetadataTooLarge`.
- **FR-011**: Errors MUST use the S3 XML error format; storage failures answer 500 `InternalError`.
- **FR-012**: The operation MUST be exercised end to end by the real S3 command-line client (constitution Principle V).
- **FR-013**: An aws-chunked upload MUST fail with 501 `NotImplemented` before any body byte is read.
- **FR-014**: A successful upload MUST return `ETag` and the request ID, and no other S3 response headers.
- **FR-015**: Objects stored before this feature MUST read as content type `binary/octet-stream` with no metadata.

### Key Entities

- **Object**: bucket, key, size, MD5 (ETag), last-modified, content type, user metadata.
- **User Metadata**: lowercase name (without the `x-amz-meta-` prefix) to value pairs.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of successful uploads return an ETag equal to the quoted MD5 of the file the client sent.
- **SC-002**: A 2 GiB upload completes with the server's peak memory below 64 MiB.
- **SC-003**: 100% of damaged or incomplete uploads (wrong MD5, early disconnect) leave no stored object and no leftover temporary data.
- **SC-004**: Every error condition in the acceptance criteria returns the specified status and error code on the first attempt.

## Assumptions

- Reading objects back (GetObject, HeadObject) is feature 009; this feature stores content type and metadata, and proves it by reading the stored record.
- No authentication: signature headers are accepted and ignored (constitution scope).
- Checksum headers sent by current clients (`x-amz-checksum-*`, `x-amz-sdk-checksum-algorithm`) are verified by feature 008, which owns checksums; this feature ignores them.
- Other standard headers (Cache-Control, Content-Disposition, Content-Encoding other than aws-chunked, Expires) are not stored.
