# Feature Specification: Durable Object Storage

**Feature Branch**: `005-durable-object-storage`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "The tiny-s3 storage engine: store, read and delete objects on local disk durably, independent of HTTP. A write either becomes fully visible with the correct size and MD5, or leaves no trace; a crash or I/O error at any step never exposes a partial object. Readers that already started reading an object keep reading the complete version they opened, even if it is overwritten or deleted meanwhile. Acceptance criteria: Put streams arbitrary-size input without buffering it in memory and returns size, hex MD5 and last-modified. Put rejects input whose length differs from the declared length (IncompleteBody) or whose MD5 differs from a supplied Content-MD5 (BadDigest), leaving nothing behind. For an injected failure at every step of the write path, no object becomes visible and no temp file remains. Get returns metadata plus an open reader; Head returns metadata only. Delete is idempotent. Concurrent Puts to the same key end with exactly one complete winner. 100% coverage using the internal/fsx fault-injection layer from spec 004."

## Clarifications

### Session 2026-11-04

Answered autonomously with the recommended option, as authorized by the feature owner for this feature only.

- Q: Which object attributes does the engine persist in this feature? → A: Size, hex MD5 and last-modified only, in a versioned metadata record so content type and user metadata can be added by the PutObject feature without a migration.
- Q: Must every Put carry a declared length, or can the length be unknown? → A: A declared length is always required; a negative length is rejected as an invalid argument before any disk write (S3 always gives a length, through Content-Length or the decoded aws-chunked length).
- Q: Does the engine enforce a maximum object size? → A: Yes, the S3 single-PUT limit: a declared length above 5 GiB fails with EntityTooLarge before any disk write.
- Q: Which keys does the engine accept? → A: Non-empty, valid UTF-8, at most 1024 bytes (S3 limits); an over-long key fails with KeyTooLongError, an empty or invalid-UTF-8 key with an invalid-argument error, both before any disk write.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Store and read back an object durably (Priority: P1)

As the S3 protocol layer (the engine's only caller), I hand the storage engine a bucket, a key, a declared length and a body stream, and get back the stored size, hex MD5 and last-modified time; afterwards I can read the object's metadata and contents back exactly as written.

**Why this priority**: Storing and retrieving objects is the core of an object store; every later S3 operation depends on it.

**Independent Test**: Put an object of known content, then Head and Get it and compare size, MD5, last-modified and body bytes; repeat with an object larger than any in-memory buffer used by the engine.

**Acceptance Scenarios**:

1. **Given** an empty store, **When** a body of N bytes is put with declared length N, **Then** the result reports size N, the lowercase hex MD5 of the body and a last-modified time, and a subsequent Head returns the same metadata.
2. **Given** a stored object, **When** it is read with Get, **Then** the caller receives the same metadata as Head plus an open reader that yields exactly the stored bytes.
3. **Given** a body much larger than the engine's copy buffer, **When** it is put, **Then** memory used by the engine stays bounded by a fixed buffer size independent of the body size.
4. **Given** a key that was never stored, **When** Head or Get is called, **Then** the engine reports that the key does not exist (NoSuchKey).

---

### User Story 2 - Reject bad input without leaving a trace (Priority: P1)

As the protocol layer, I need the engine to refuse a body whose length or MD5 does not match what the client declared, so that no truncated or corrupted object is ever stored.

**Why this priority**: Accepting a truncated or corrupted body would silently lose client data, which violates the durability principle.

**Independent Test**: Put bodies that are shorter than, longer than, or differ in MD5 from the declared values, and verify the specific error, that no object is visible, and that no temporary file remains.

**Acceptance Scenarios**:

1. **Given** a declared length N, **When** the body ends before N bytes, **Then** Put fails with IncompleteBody, the key is not visible, and no temporary file remains.
2. **Given** a declared length N, **When** the body contains more than N bytes, **Then** Put fails with IncompleteBody, the key is not visible, and no temporary file remains.
3. **Given** a supplied Content-MD5, **When** the body's MD5 differs, **Then** Put fails with BadDigest and leaves nothing behind.
4. **Given** an existing object under the key, **When** a rejected Put targets the same key, **Then** the existing object remains unchanged and readable.

---

### User Story 3 - Survive I/O failures and crashes at any write step (Priority: P1)

As the operator, I need the engine to guarantee that an I/O error or crash at any step of a write leaves either the complete new object or the previous state, never a partial object.

**Why this priority**: This is the constitution's non-negotiable durability guarantee.

**Independent Test**: Using the filesystem fault-injection layer, fail each operation on the write path in turn (temp create, each write, file sync, rename, directory open and sync, metadata create/write/sync/rename/directory sync) and verify the error, the absence of a visible new object, the previous object's integrity, and the absence of temp files.

**Acceptance Scenarios**:

1. **Given** an injected failure at any single write-path step, **When** Put runs, **Then** Put returns an error, no new object version becomes visible, any previous version stays intact, and no temporary file remains.
2. **Given** a successful Put, **When** the recorded operation history is inspected, **Then** the body is written, file-synced, renamed into place and its directory synced before the metadata record is written, synced, renamed and its directory synced, and success is returned only after the metadata directory sync.
3. **Given** a process that stopped mid-write (simulated by temp files left in the temporary area), **When** the store is opened again, **Then** the leftovers are removed and never visible as objects.

---

### User Story 4 - Consistent reads under concurrent overwrite and delete (Priority: P2)

As a client reading a large object, I keep receiving the complete version I opened even if another client overwrites or deletes that key while I read; concurrent writers to the same key never produce a mixed object.

**Why this priority**: Concurrency correctness matters as soon as more than one client uses the store, but it builds on Stories 1–3.

**Independent Test**: Open a reader, overwrite and then delete the key, and verify the reader still yields the full original bytes; run many concurrent Puts with distinct bodies to one key under the race detector and verify the final object matches exactly one of them in size, MD5 and contents.

**Acceptance Scenarios**:

1. **Given** an open reader on version A, **When** the key is overwritten with version B, **Then** the reader still yields exactly A's bytes, and a new Get yields B.
2. **Given** an open reader on an object, **When** the key is deleted, **Then** the reader still yields the complete object, and a new Head reports NoSuchKey.
3. **Given** K concurrent Puts to the same key with distinct bodies, **When** they all finish, **Then** the stored object's metadata and contents match exactly one of the bodies and no temporary or orphaned data from the losers remains.

---

### User Story 5 - Idempotent delete (Priority: P2)

As the protocol layer, I can delete a key whether or not it exists and always get success, matching S3's DeleteObject semantics.

**Why this priority**: Required for S3 compatibility; simple once storage exists.

**Independent Test**: Delete a stored key twice and a never-stored key once; all calls succeed and Head reports NoSuchKey afterwards.

**Acceptance Scenarios**:

1. **Given** a stored object, **When** it is deleted, **Then** Delete succeeds and Head reports NoSuchKey.
2. **Given** a key that does not exist, **When** it is deleted, **Then** Delete succeeds.
3. **Given** an injected failure while removing the metadata record, **When** Delete runs, **Then** Delete returns an error and the object remains fully readable.
4. **Given** an injected failure while syncing the metadata directory after the record was removed, **When** Delete runs, **Then** Delete returns an error and keeps the data file, so a record that reappears after a crash still points at a complete body.

### Edge Cases

- Zero-length object: Put with declared length 0 and an empty body succeeds with the MD5 of empty input.
- Keys containing `/`, `..`, spaces, non-ASCII UTF-8 or other characters that are unsafe in file names must be stored and retrieved without touching paths outside the store.
- A key longer than 1024 bytes fails with KeyTooLongError; an empty key or a key that is not valid UTF-8 fails with an invalid-argument error; in both cases nothing is written.
- A negative declared length fails with an invalid-argument error; a declared length above 5 GiB fails with EntityTooLarge; in both cases nothing is written.
- Same key in different buckets refers to different objects.
- The body reader returns an error mid-stream: Put fails with that error, nothing is left behind.
- The caller's context is cancelled mid-stream: Put stops, fails, and leaves nothing behind.
- Removing the temporary file during cleanup also fails: the original error is returned, not the cleanup error.
- After a successful overwrite, removing the previous version's data fails: the Put still succeeds (the new version is committed) and the leftover is invisible to readers.
- A metadata record that cannot be decoded is reported by Head and Get as an internal error, not as a missing key; Put replaces it and Delete removes it, so the key can be repaired.
- A short write without an error is treated as a failure, never as a complete write.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The engine MUST expose Put, Get, Head and Delete operations addressed by bucket and key, callable without any HTTP machinery.
- **FR-002**: Put MUST stream the body to disk with a fixed-size buffer; memory per Put MUST NOT depend on the object size.
- **FR-003**: Put MUST return the stored size, the lowercase hex MD5 of the body and the last-modified time.
- **FR-004**: Put MUST fail with IncompleteBody when the body has fewer or more bytes than the declared length, and with BadDigest when a supplied Content-MD5 does not match the body's MD5.
- **FR-005**: Put MUST follow the constitution's write order: body to a temp file on the same filesystem, file sync, atomic rename into place, parent-directory sync, then the durable metadata record (temp, sync, rename, directory sync). The metadata record is the commit point.
- **FR-006**: Any failure on the write path, including rejected input, MUST remove every file the Put created and return an error; no new version may become visible and a previous version MUST remain intact.
- **FR-007**: Get MUST return the object's metadata together with an open reader over exactly the committed bytes; Head MUST return the same metadata without opening the body.
- **FR-008**: A reader obtained from Get MUST keep yielding the complete version it opened even if the key is later overwritten or deleted.
- **FR-009**: Delete MUST succeed whether or not the key exists; after a successful Delete, Head and Get MUST report NoSuchKey.
- **FR-010**: Concurrent Puts and Deletes on the same key MUST serialize their commits so that the final state is exactly one complete version (or absence), never a mix, and no data from losing writers remains.
- **FR-011**: Keys MUST be mapped to storage locations so that no key can address a path outside the store, regardless of its characters.
- **FR-012**: Opening the store MUST create its directory layout if missing and remove leftover temporary files from interrupted writes.
- **FR-013**: All filesystem operations of the engine MUST go through the `internal/fsx` layer so every failure branch is testable with fault injection.
- **FR-014**: Put MUST require a declared length between 0 and 5 GiB inclusive; a negative length is an invalid argument and a larger length fails with EntityTooLarge, before any disk write.
- **FR-015**: Put, Get, Head and Delete MUST validate the key before touching disk: non-empty, valid UTF-8, at most 1024 bytes; over-long keys fail with KeyTooLongError, other invalid keys with an invalid-argument error.
- **FR-016**: The metadata record MUST carry a format version and, in this feature, exactly the bucket, key, size, hex MD5, last-modified time and the data version it points to; a record with an unknown format version is reported as an internal error.
- **FR-017**: The engine package and every other package under `internal/` MUST have 100% statement coverage, and all tests MUST pass with the race detector.

### Key Entities

- **Object**: A stored body identified by bucket and key, with size, MD5 and last-modified time.
- **Object Info**: The metadata returned by Put, Head and Get: bucket, key, size, hex MD5, last-modified.
- **Metadata Record**: The durable per-key record whose presence defines that the object exists; it holds a format version, bucket, key, size, hex MD5, last-modified and the data version it points to. Content type and user metadata are deferred to the PutObject feature.
- **Data Version**: An immutable file holding one committed body; each Put creates a new one.
- **Temporary File**: A file being written by an in-flight Put; never visible as an object.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For every step of the write path (each injectable operation in the successful Put history), a test injecting a failure at that step finds zero visible new versions and zero temporary files afterwards.
- **SC-002**: A 64 MiB Put allocates less than 4 MiB in total (no allocation proportional to body size), verified by a test.
- **SC-003**: 100% of Puts with a length or MD5 mismatch fail with the specified error and leave zero files behind.
- **SC-004**: In a run of at least 16 concurrent Puts to one key, the final object matches exactly one writer's body in size, MD5 and bytes, with zero leftover temporary or orphaned data files, under the race detector.
- **SC-005**: An open reader returns 100% of the original bytes after the key is overwritten and after it is deleted.
- **SC-006**: Every package under `internal/` reports 100% statement coverage in `make cover`.

## Assumptions

- The engine is a Go package used in-process by the future S3 HTTP handlers; this feature does not add or change any S3 endpoint, so no new `aws s3api` e2e test is required yet.
- The caller (the HTTP layer) decodes the base64 `Content-MD5` header and passes the raw digest; malformed headers (InvalidDigest) are the caller's concern.
- Bucket existence and bucket naming are handled by the routing and future bucket features; the engine treats the bucket as a namespace component.
- Only one s3d process uses a data directory at a time (multi-node is out of scope), so in-process locking is sufficient for concurrency.
- Last-modified is the wall-clock time at commit, in UTC.
- Orphaned committed data files left by a crash between the data rename and the metadata commit are invisible to readers; reclaiming them belongs to the future index-rebuild feature.
