# Feature Specification: Crash Recovery and Concurrency Guarantees

**Feature Branch**: `013-crash-recovery`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "Crash recovery and concurrency guarantees for tiny-s3. After any crash, including kill -9 in the middle of uploads, the server restarts into a consistent state: every acknowledged object is readable and listed, no partial object is visible, and leftover temporary files and orphaned blobs are cleaned up. Under concurrent load on the same key, the store stays consistent. Acceptance criteria: Killing the server with SIGKILL during 20 parallel uploads, then restarting, leaves only complete objects whose bytes match their ETag. tmp/ is empty after startup. Blobs not referenced by any metadata are removed at startup. The in-memory index is rebuilt from metadata and matches a full disk scan. 50 concurrent PUTs to one key end with that key holding exactly one of the uploaded bodies. Startup logs how many temp files and orphans were removed and how many objects were indexed."

## Clarifications

### Session 2026-11-05

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: What happens to a metadata record whose object body is missing? → A: It is left out of the listing and counted as skipped in the startup log; it is not deleted automatically (reading it answers 500, and uploading or deleting the key repairs it), so outside interference is reported, not hidden.
- Q: Which entries in the body area count as unreferenced? → A: Every entry not named by a valid metadata record, whatever its name; the area only ever holds bodies the server wrote.
- Q: Are the startup removals made durable with a directory sync? → A: No. Cleanup is idempotent: if a crash undoes a removal, the next startup removes it again. Only the write path needs ordering guarantees.
- Q: What does the startup log line contain, and when is it written? → A: One line, `recovery`, written every startup before the server listens, with `tmp_removed`, `orphans_removed`, `objects_indexed`, `records_skipped` and `buckets`; zeros are logged too.
- Q: Which records count as skipped? → A: Records that cannot be decoded, records under a file name that is not their key's hash (feature 012), and records whose body is missing.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Restart cleanly after a crash during uploads (Priority: P1)

As an operator, when the server is killed abruptly while clients are uploading, I restart it and find every upload that was acknowledged readable and listed with the right bytes, and nothing half-written visible.

**Why this priority**: This is the durability promise of the whole project, proved under the harshest stop.

**Independent Test**: Start 20 parallel uploads, let some finish, kill the server without a clean shutdown while the rest are in flight, restart it, and check every listed object.

**Acceptance Scenarios**:

1. **Given** 20 parallel uploads, some acknowledged and some still sending, **When** the server is killed abruptly and restarted, **Then** every acknowledged upload is listed and readable with bytes whose MD5 equals its ETag.
2. **Given** the same restart, **When** the client lists the bucket, **Then** no upload that was not acknowledged appears, and every listed object downloads completely with bytes matching its ETag.
3. **Given** a restart after the kill, **When** the store is inspected, **Then** the temporary area is empty.

---

### User Story 2 - Reclaim space left by crashes (Priority: P1)

As an operator, disk space taken by uploads that never completed, or by object bodies no longer referenced, is reclaimed when the server starts, and the startup log tells me how much was cleaned.

**Why this priority**: Without cleanup, every crash leaks disk space forever.

**Independent Test**: Plant leftover temporary files and unreferenced object bodies, start the server, and check they are gone and counted in the startup log.

**Acceptance Scenarios**:

1. **Given** leftover temporary files, **When** the server starts, **Then** they are removed and their number appears in the startup log.
2. **Given** object bodies that no metadata record references, **When** the server starts, **Then** they are removed and their number appears in the startup log; bodies referenced by records are kept.
3. **Given** a normal restart, **When** the server starts, **Then** the startup log reports the number of objects indexed, and zero removals.

---

### User Story 3 - The index matches the disk (Priority: P1)

As an operator, the listing the server serves after a restart is exactly what the durable records on disk say.

**Why this priority**: The index is the only source of listings; if it drifts from the records, clients see ghosts or miss objects.

**Independent Test**: After a crash and restart, compare the full listing with a scan of every metadata record on disk.

**Acceptance Scenarios**:

1. **Given** a store after any sequence of writes, deletes and crashes, **When** the server starts, **Then** the set of listed objects (with size and ETag) equals the set of valid metadata records on disk.

---

### User Story 4 - Concurrent writes to one key (Priority: P2)

As a client among many writing the same key at once, the key ends up holding exactly one of the uploaded bodies, never a mix, and no space is leaked by the losers.

**Why this priority**: Correctness under contention; the engine already serializes commits, this story proves it end to end at the size requested.

**Independent Test**: Send 50 concurrent uploads with distinct bodies to one key, then read it back.

**Acceptance Scenarios**:

1. **Given** 50 concurrent uploads of distinct bodies to one key, **When** they all finish, **Then** the key's bytes equal exactly one of the bodies, its ETag matches those bytes, and the store holds exactly one body file for the key.

### Edge Cases

- A crash between writing an object's body and its metadata record leaves an unreferenced body: it is removed at the next start.
- A crash while overwriting leaves either the old or the new version, complete.
- A metadata record that cannot be decoded is left out of the listing (feature 012); the body it pointed to cannot be identified and is removed as unreferenced.
- A metadata record whose body is missing (outside interference) is left out of the listing and counted in the startup log.
- Cleanup failures (a body or temporary file that cannot be removed) stop the startup with an error naming the file, rather than serving with unknown state.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: After any crash, including an abrupt kill during uploads, every acknowledged upload MUST be listed and readable with bytes matching its ETag, and no unacknowledged or partial upload may be visible.
- **FR-002**: At startup the temporary area MUST be emptied.
- **FR-003**: At startup every object body not referenced by a valid metadata record MUST be removed.
- **FR-004**: At startup the index MUST be rebuilt from the metadata records and MUST equal a full scan of them.
- **FR-005**: Concurrent uploads to one key MUST leave exactly one complete body, with no leftover body from the other uploads.
- **FR-006**: Startup MUST log, in one structured line named `recovery` written before the server listens, the number of temporary files removed, unreferenced bodies removed, objects indexed, records skipped and buckets loaded.
- **FR-007**: A failure to remove a leftover MUST stop startup with an error naming the file.
- **FR-008**: The crash and concurrency guarantees MUST be exercised end to end with the real server process and a real abrupt kill, and verified with the S3 command-line client where it applies (constitution Principle V).
- **FR-009**: A metadata record whose body is missing MUST be left out of the listing, counted as skipped, and kept on disk.
- **FR-010**: Startup removals MUST NOT need a directory sync; cleanup MUST be idempotent across crashes.

### Key Entities

- **Temporary file**: a file of an in-flight write in the temporary area.
- **Object body (blob)**: an immutable data file; referenced by at most one metadata record.
- **Metadata record**: the commit point of an object (feature 005).
- **Recovery report**: counts of temporary files removed, unreferenced bodies removed, objects indexed and records skipped.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of kill-during-upload runs, every acknowledged upload is readable with a matching ETag and no unacknowledged one is listed.
- **SC-002**: After every startup, the temporary area holds 0 files and the number of object bodies equals the number of listed objects.
- **SC-003**: The listing after a restart equals the scan of the metadata records in 100% of runs.
- **SC-004**: 50 concurrent uploads to one key end with exactly 1 body stored and it equals one of the 50 inputs.

## Assumptions

- An abrupt kill keeps the operating system's page cache, so these tests prove that acknowledged state is on the filesystem in the right order; power-loss durability relies on the fsync order that feature 005 tests operation by operation.
- One server process owns a data directory, so cleanup at startup cannot race with writers.
- Verifying every body's MD5 at startup is out of scope (it would read all data); bodies are complete by construction because they are renamed into place only after being synced.
