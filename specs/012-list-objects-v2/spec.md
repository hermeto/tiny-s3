# Feature Specification: Ordered Index and ListObjectsV2

**Feature Branch**: `012-list-objects-v2`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "ListObjectsV2 for tiny-s3 so clients can browse a bucket by prefix and page through large listings. Listing results reflect every acknowledged write and delete immediately. Acceptance criteria: aws s3api list-objects-v2 --bucket demo lists keys in UTF-8 binary order with Key, Size, ETag, LastModified and StorageClass STANDARD. --prefix filters. --delimiter / groups keys into CommonPrefixes, so aws s3 ls s3://demo/dir/ shows folders. max-keys (default 1000, max 1000) limits each page and sets IsTruncated and NextContinuationToken. Following tokens over 2,500 keys returns every key exactly once, even while other clients write to the bucket. start-after works. encoding-type=url returns URL-encoded keys and prefixes, which the aws CLI requests by default. KeyCount is correct. Listing a missing bucket returns 404 NoSuchBucket."

## Clarifications

### Session 2026-11-05

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: What does `max-keys=0` return? → A: An empty page with `KeyCount` 0, `IsTruncated` false and no token, so a client loop always ends.
- Q: Which wins when both `continuation-token` and `start-after` are sent? → A: The token, as in S3; `start-after` is still echoed in the response.
- Q: What is a continuation token? → A: An opaque string that encodes where the last page ended (a key, or a whole common prefix). It does not depend on server memory, so it stays valid across restarts; an undecodable token fails with 400 `InvalidArgument`.
- Q: How does `start-after` interact with `delimiter`? → A: It is a plain key: only keys strictly after it are considered, then grouped; a common prefix appears if it has a key after `start-after`.
- Q: What happens at startup with a metadata record that cannot be decoded? → A: The server still starts; that key is not listed. Reading it keeps answering 500 and uploading or deleting it repairs it (the 005 engine's rules).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - List a bucket (Priority: P1)

As an S3 client user, I list the objects in a bucket and see every key in sorted order with its size, ETag, last-modified date and storage class.

**Why this priority**: Listing is how clients discover what is stored; sync tools and `aws s3 ls` depend on it.

**Independent Test**: Upload keys in random order, list them with the S3 command-line client and compare against the expected sorted list.

**Acceptance Scenarios**:

1. **Given** objects uploaded in any order, **When** the client lists the bucket, **Then** every key appears once, in UTF-8 byte order, with its Key, Size, ETag, LastModified and StorageClass `STANDARD`, and `KeyCount` equals the number of entries returned.
2. **Given** an empty bucket, **When** the client lists it, **Then** the result has no entries and `KeyCount` 0.
3. **Given** a bucket that does not exist, **When** the client lists it, **Then** the request fails with 404 `NoSuchBucket`.
4. **Given** an object was just uploaded or deleted and the request acknowledged, **When** the client lists the bucket, **Then** the listing already reflects it.
5. **Given** objects stored before a server restart, **When** the client lists after the restart, **Then** they are all listed.

---

### User Story 2 - Browse by prefix and folders (Priority: P1)

As an S3 client user, I list only the keys under a prefix and see the next level of "folders" grouped, so `aws s3 ls s3://demo/dir/` shows files and subfolders like a directory listing.

**Why this priority**: Prefix and delimiter browsing is how humans and tools navigate buckets.

**Independent Test**: Store a small tree of keys and list with prefix and delimiter through `aws s3api` and `aws s3 ls`.

**Acceptance Scenarios**:

1. **Given** keys `a/1`, `a/2`, `b/1`, **When** the client lists with prefix `a/`, **Then** only `a/1` and `a/2` are returned.
2. **Given** keys `dir/f1`, `dir/sub/f2`, `dir/sub/f3`, `top`, **When** the client lists with prefix `dir/` and delimiter `/`, **Then** `dir/f1` is listed as a key and `dir/sub/` as a common prefix, each common prefix counted once in `KeyCount`.
3. **Given** the same keys, **When** the user runs `aws s3 ls s3://demo/dir/`, **Then** it shows `PRE sub/` and `f1`.
4. **Given** keys with spaces, `+`, `%` and non-ASCII characters, **When** the command-line client lists them (it asks for URL encoding by default), **Then** it shows the keys exactly as stored.

---

### User Story 3 - Page through large listings (Priority: P1)

As an S3 client user, I page through a bucket with thousands of keys and get every key exactly once, even while other clients keep writing.

**Why this priority**: Paging is required for any bucket over 1,000 keys; duplicates or gaps break sync tools.

**Independent Test**: Store 2,500 keys, page through them with `max-keys` and continuation tokens while another client uploads new keys and overwrites existing ones, and check that every original key appears exactly once.

**Acceptance Scenarios**:

1. **Given** more keys than `max-keys`, **When** the client lists, **Then** the page holds exactly `max-keys` entries, `IsTruncated` is true and a `NextContinuationToken` is returned.
2. **Given** a continuation token, **When** the client asks for the next page, **Then** it continues right after the last entry of the previous page.
3. **Given** 2,500 keys and concurrent writes during the listing, **When** the client follows every token, **Then** each of the 2,500 keys appears exactly once.
4. **Given** `start-after` K, **When** the client lists, **Then** only keys after K are returned.
5. **Given** no `max-keys`, or a value above 1,000, **When** the client lists, **Then** pages hold at most 1,000 entries.

### Edge Cases

- A common prefix that spans a page boundary appears once: the next page continues after the whole group.
- A delimiter of more than one character works the same way as `/`.
- An invalid `max-keys` (not a non-negative integer) fails with 400 `InvalidArgument`; an invalid continuation token fails with 400 `InvalidArgument`.
- `encoding-type` other than `url` fails with 400 `InvalidArgument`.
- A key that is deleted after the client received a page does not affect later pages; a key added before the cursor is not shown in this listing.
- A corrupt metadata record does not stop the server from starting; its key is not listed.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Users MUST be able to list a bucket's objects in UTF-8 binary order, with Key, Size, ETag, LastModified and StorageClass `STANDARD` per entry, and `KeyCount` equal to the number of keys plus common prefixes returned.
- **FR-002**: Listing MUST support `prefix`, `delimiter` (grouping into `CommonPrefixes`), `start-after`, `max-keys` (default 1,000, capped at 1,000) and continuation tokens (`IsTruncated`, `NextContinuationToken`).
- **FR-003**: A listing MUST reflect every acknowledged write and delete immediately, and every stored object after a restart.
- **FR-004**: Following continuation tokens MUST return every key that exists throughout the listing exactly once, even under concurrent writes.
- **FR-005**: `encoding-type=url` MUST URL-encode keys, prefixes, delimiter and start-after in the response, so that any decoder of URL encoding gets the original values.
- **FR-006**: A missing bucket MUST answer 404 `NoSuchBucket`; invalid parameters MUST answer 400 `InvalidArgument`.
- **FR-007**: Listing MUST NOT read object data or metadata files from disk per request; its cost grows with the number of entries returned and scanned, not with object sizes.
- **FR-008**: Listing MUST be exercised end to end by the real S3 command-line client, including `aws s3 ls` (constitution Principle V).
- **FR-009**: `max-keys=0` MUST return an empty, non-truncated page; a continuation token MUST take precedence over `start-after`; tokens MUST be opaque, independent of server memory, and rejected with 400 `InvalidArgument` when undecodable.
- **FR-010**: At startup, objects with undecodable records MUST be left out of the listing without stopping the server.

### Key Entities

- **Index entry**: bucket, key, size, ETag, last-modified; kept in memory, ordered by key, rebuilt from the durable metadata records at startup (constitution: Scope and Constraints).
- **Continuation token**: an opaque value that marks where the previous page ended.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Paging through 2,500 keys with concurrent writes returns each key exactly once in 100% of runs.
- **SC-002**: 100% of listings taken right after an acknowledged write or delete reflect it.
- **SC-003**: Every scenario in User Stories 1–3 returns the specified entries and counts with the command-line client.
- **SC-004**: Listing a page of 1,000 keys from a bucket of 10,000 keys answers within 100 ms on the development machine.

## Assumptions

- ListObjects v1 (no `list-type=2`) stays 501.
- `fetch-owner` and `x-amz-request-payer` are ignored; no owner is returned.
- The index lives in memory, as the constitution states; crash recovery details (orphan cleanup, startup logging, full-scan checks) are feature 013.
- Keys with characters that are not valid in XML 1.0 are only listable with `encoding-type=url`.
