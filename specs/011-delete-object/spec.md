# Feature Specification: DeleteObject

**Feature Branch**: `011-delete-object`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "DeleteObject for tiny-s3 with S3's idempotent semantics. A client deletes an object; once the delete is acknowledged, the object is gone from GET, HEAD and LIST, also after a restart. Acceptance criteria: aws s3api delete-object returns 204 whether or not the key existed. A deleted key returns 404 NoSuchKey afterwards. A GET in progress when the delete arrives completes with the full original bytes. A crash after the delete is acknowledged never brings the object back. Deleting from a missing bucket returns 404 NoSuchBucket."

## Clarifications

### Session 2026-11-05

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features. Three questions were enough; the rest of the behavior is fixed by the 005 engine.

- Q: What does a delete of a key whose stored record is corrupt do? → A: It removes the record and answers 204, so the key can be repaired (the 005 engine's behavior); its data file, if any, becomes an orphan for crash recovery (feature 013).
- Q: Are conditional headers (`If-Match`) on DeleteObject honored? → A: No, they are ignored; S3 supports them only in newer, narrower cases, and no default client sends them.
- Q: Which headers does a successful delete return? → A: Status 204, the request ID and no body; no delete-marker or version headers.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Delete an object (Priority: P1)

As an S3 client user, I delete an object and it is gone: reading it or its metadata reports that the key does not exist. Deleting a key that does not exist also succeeds, so my scripts can retry deletes safely.

**Why this priority**: Deleting is the last basic object operation; idempotency is what S3 clients rely on.

**Independent Test**: Upload an object, delete it twice with the S3 command-line client, then try to read it.

**Acceptance Scenarios**:

1. **Given** an existing object, **When** the client deletes it, **Then** the request succeeds with 204, and a later download or metadata request answers 404 `NoSuchKey`.
2. **Given** a key that does not exist, **When** the client deletes it, **Then** the request succeeds with 204.
3. **Given** a bucket that does not exist, **When** the client deletes a key in it, **Then** the request fails with 404 `NoSuchBucket`.
4. **Given** a key longer than 1024 bytes, **When** the client deletes it, **Then** the request fails with 400 `KeyTooLongError`.

---

### User Story 2 - Deletes are durable (Priority: P1)

As an operator, once a delete is acknowledged the object never comes back, even if the server crashes right after.

**Why this priority**: A deleted object reappearing after a crash breaks client expectations as badly as a lost write.

**Independent Test**: Delete an object, kill the server without a clean shutdown immediately after the acknowledgement, restart it, and read the key.

**Acceptance Scenarios**:

1. **Given** an acknowledged delete, **When** the server is killed abruptly and restarted, **Then** the key answers 404 `NoSuchKey`.
2. **Given** a storage failure while removing the object's record, **When** the client deletes, **Then** the request fails with 500 `InternalError` and the object stays fully readable.
3. **Given** a storage failure while making an already-removed record durable, **When** the client deletes, **Then** the request fails with 500 `InternalError`; the object may be gone, or come back after a crash, but whenever it is visible it is complete.

---

### User Story 3 - Downloads in progress are not cut (Priority: P2)

As an S3 client user, a download that started before someone deleted the object still completes with all of its bytes.

**Why this priority**: Read consistency under concurrent deletes; the engine already provides it, this story proves it end to end.

**Independent Test**: Start a large download, delete the object mid-transfer, finish the download and compare.

**Acceptance Scenarios**:

1. **Given** a download in progress, **When** the object is deleted, **Then** the download completes with exactly the original bytes and later reads answer 404.

### Edge Cases

- Deleting the same key many times in a row always answers 204.
- An invalid bucket name answers `InvalidBucketName` (feature 006).
- Listing (feature 012) must not show a deleted key; this feature only has to keep the stored state correct for it.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Users MUST be able to delete an object; success answers 204 with no body, whether or not the key existed.
- **FR-002**: After an acknowledged delete, downloads and metadata requests for the key MUST answer 404 `NoSuchKey`, including after a restart or crash.
- **FR-003**: The server MUST answer the delete only after the removal is durable. A failure to remove the record MUST answer 500 `InternalError` and leave the object readable; a failure to make the removal durable MUST answer 500 `InternalError` and keep the object's body, so an object that reappears after a crash is complete.
- **FR-004**: A delete MUST NOT interrupt downloads already in progress.
- **FR-005**: A missing bucket MUST answer 404 `NoSuchBucket`; a key over 1024 bytes 400 `KeyTooLongError`.
- **FR-006**: The operation MUST be exercised end to end by the real S3 command-line client, including a crash right after an acknowledged delete (constitution Principle V).
- **FR-007**: A delete of a key with a corrupt record MUST remove the record and answer 204.
- **FR-008**: Conditional headers on DeleteObject MUST be ignored; a success MUST carry only the request ID.

### Key Entities

- **Object** (005): removed by deleting its metadata record, the commit point.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of deletes answer 204, for existing and missing keys.
- **SC-002**: After an acknowledged delete and an abrupt kill, 100% of restarts report the key as missing.
- **SC-003**: A download in progress during a delete returns 100% of the original bytes.

## Assumptions

- No versioning: a delete removes the only version; no delete markers or `x-amz-delete-marker` header.
- Multi-object delete (`POST ?delete`) is out of scope and keeps answering 501.
- The storage engine's delete (feature 005) already syncs the removal before returning; this feature wires it to HTTP and proves the guarantees end to end.
