# Feature Specification: Minimal Bucket Support

**Feature Branch**: `006-minimal-bucket-support`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Minimal bucket support so the real aws CLI can create and check a bucket before working with objects. A user can create a bucket, check that it exists, and list buckets. Acceptance criteria: aws s3api create-bucket --bucket demo returns 200, and the bucket survives a server restart. Creating it again returns 409 BucketAlreadyOwnedByYou. aws s3api head-bucket returns 200 for an existing bucket and 404 otherwise. aws s3api list-buckets lists created buckets with creation dates. Every object operation on a missing bucket returns 404 NoSuchBucket. Bucket names that are not valid S3 names return 400 InvalidBucketName. Out of scope: DeleteBucket, bucket policies, regions other than us-east-1."

## Clarifications

### Session 2026-11-04

Answered autonomously with the recommended option, as authorized by the feature owner for this feature only.

- Q: Which bucket names are valid? → A: The current S3 rules for general purpose buckets: 3–63 characters; lowercase letters, digits, dots and hyphens; starts and ends with a letter or digit; no two adjacent dots; not formatted as an IPv4 address; not starting with `xn--`, `sthree-` or `amzn-s3-demo-`; not ending with `-s3alias`, `--ol-s3`, `.mrap`, `--x-s3` or `--table-s3`. Legacy us-east-1 names (uppercase, underscores) are not accepted.
- Q: Is the bucket name checked only on create, or on every request that names a bucket? → A: On every request that names a bucket, before the existence check: an invalid name answers 400 `InvalidBucketName` (status only for HEAD), so a bad name never reads as a missing bucket.
- Q: What happens when a create request asks for a region other than us-east-1? → A: A create request whose body names a location other than us-east-1 fails with 400 `InvalidLocationConstraint` and creates nothing; no body, an empty location or `us-east-1` means us-east-1; a body that is not a valid create-bucket configuration fails with 400 `MalformedXML`.
- Q: In what order and how many buckets does a list return? → A: All buckets in one response, sorted by name ascending; list filtering and paging parameters are ignored.
- Q: What does the server do at startup when a stored bucket record cannot be read or is invalid? → A: It refuses to start and reports the offending record, rather than silently hiding a bucket.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Create a bucket that survives a restart (Priority: P1)

As an S3 client user, I create a bucket with the standard S3 command-line client, so that I have a place to store objects; the bucket is still there after the server restarts.

**Why this priority**: No object can be stored without a bucket, and real clients create one first. Losing a bucket on restart would make every later feature unusable.

**Independent Test**: Create a bucket with the S3 command-line client, restart the server on the same data, and check that the bucket still exists.

**Acceptance Scenarios**:

1. **Given** a server with no buckets, **When** the client creates bucket `demo`, **Then** the request succeeds with status 200.
2. **Given** bucket `demo` was created, **When** the server is stopped and started again on the same data, **Then** `demo` still exists with the same creation date.
3. **Given** bucket `demo` exists, **When** the client creates `demo` again, **Then** the request fails with status 409 and error code `BucketAlreadyOwnedByYou`, and the original creation date is unchanged.
4. **Given** any server state, **When** the client creates a bucket whose name is not a valid S3 bucket name, **Then** the request fails with status 400 and error code `InvalidBucketName`, and no bucket is created.

---

### User Story 2 - Check that a bucket exists (Priority: P1)

As an S3 client user, I check whether a bucket exists before working with it.

**Why this priority**: Clients and scripts check a bucket before using it; it is the cheapest way to confirm setup.

**Independent Test**: Check an existing and a missing bucket with the S3 command-line client and compare the outcomes.

**Acceptance Scenarios**:

1. **Given** bucket `demo` exists, **When** the client checks `demo`, **Then** the request succeeds with status 200.
2. **Given** bucket `missing` does not exist, **When** the client checks it, **Then** the request fails with status 404.

---

### User Story 3 - List buckets (Priority: P2)

As an S3 client user, I list all buckets with their creation dates.

**Why this priority**: Useful for discovery and for confirming state, but clients can work without it.

**Independent Test**: Create two buckets, list buckets with the S3 command-line client, and check both names and creation dates.

**Acceptance Scenarios**:

1. **Given** no buckets, **When** the client lists buckets, **Then** the list is empty and the request succeeds.
2. **Given** buckets `beta` and `alpha` were created in that order, **When** the client lists buckets, **Then** both appear in name order (`alpha`, `beta`), each with its creation date.

---

### User Story 4 - Object operations require an existing bucket (Priority: P1)

As an S3 client user, any object operation I send to a bucket that does not exist fails clearly with `NoSuchBucket`, so I learn I forgot to create it.

**Why this priority**: This is the contract every later object feature builds on.

**Independent Test**: Send put, get, head, delete and list-objects requests to a bucket that was never created, and check each returns 404 `NoSuchBucket`.

**Acceptance Scenarios**:

1. **Given** bucket `missing` does not exist, **When** the client puts, gets, deletes or lists objects in it, **Then** each request fails with status 404 and error code `NoSuchBucket`.
2. **Given** bucket `missing` does not exist, **When** the client checks an object's metadata in it, **Then** the request fails with status 404.

### Edge Cases

- Name length limits: names shorter than 3 or longer than 63 characters are invalid.
- Characters: only lowercase letters, digits, dots and hyphens; the name starts and ends with a letter or digit; two adjacent dots are invalid; a name formatted like an IPv4 address (`192.168.5.4`) is invalid; reserved prefixes and suffixes defined by S3 are invalid.
- Two clients create the same bucket at the same moment: exactly one succeeds and the other gets `BucketAlreadyOwnedByYou`.
- A storage failure while creating a bucket: the request fails with an internal error and the bucket does not exist afterwards (in this run or after a restart), unless cleanup itself fails, in which case the bucket may exist but is never half-created.
- The server is restarted many times: bucket names and creation dates never change.
- A request to create a bucket in a region other than us-east-1 is rejected with `InvalidLocationConstraint` rather than silently creating the bucket in us-east-1; an unreadable create body is rejected with `MalformedXML`.
- A check or object request naming an invalid bucket answers `InvalidBucketName`, not `NoSuchBucket`.
- A stored bucket record that is unreadable or invalid at startup stops the server with an error naming the record.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Users MUST be able to create a bucket by name; success returns status 200.
- **FR-002**: Creating a bucket that already exists MUST fail with status 409 and error code `BucketAlreadyOwnedByYou`, leaving the existing bucket unchanged.
- **FR-003**: A created bucket and its creation date MUST be durable: once creation succeeds, the bucket exists after any later restart or crash.
- **FR-004**: Checking a bucket MUST return status 200 when it exists and 404 when it does not.
- **FR-005**: Listing buckets MUST return every created bucket with its name and creation date.
- **FR-006**: Every object operation (put, get, head, delete, list objects) addressed to a bucket that does not exist MUST fail with status 404 and error code `NoSuchBucket` (status only for requests without a response body).
- **FR-007**: Every request naming a bucket that is not a valid S3 bucket name (rules in Clarifications) MUST fail with status 400 and error code `InvalidBucketName` before the bucket's existence is checked.
- **FR-008**: All errors MUST use the S3 error format already used by the server (code, message, request ID).
- **FR-009**: Concurrent creations of the same bucket MUST result in exactly one success.
- **FR-010**: Each bucket operation MUST be exercised end to end by the real S3 command-line client (constitution Principle V).
- **FR-011**: A create request MUST be rejected with 400 `InvalidLocationConstraint` when its body names a location other than us-east-1, and with 400 `MalformedXML` when the body cannot be parsed; a create with no body, no location or an empty location creates the bucket in us-east-1.
- **FR-012**: Listing buckets MUST return all buckets in one response, sorted by name ascending.
- **FR-013**: At startup the server MUST load every stored bucket and MUST refuse to start, naming the record, when one cannot be read or is invalid.

### Key Entities

- **Bucket**: A named container for objects. Attributes: name (unique, valid S3 name) and creation date (set once, at creation).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user goes from an empty server to a confirmed bucket in two client commands (create, then check), both succeeding on the first attempt.
- **SC-002**: 100% of created buckets, with unchanged creation dates, are present after a restart.
- **SC-003**: 100% of object requests to missing buckets fail with `NoSuchBucket`, across all five object operations.
- **SC-004**: 100% of tested invalid names, covering every naming rule, are rejected with `InvalidBucketName`, and 100% of tested valid names are accepted.
- **SC-005**: In a run of at least 16 concurrent creations of one bucket, exactly one succeeds.

## Assumptions

- There is a single owner: no authentication, so every bucket belongs to the same implicit user, and creating an existing bucket is always "already owned by you".
- The server runs in us-east-1 only.
- Object operations other than the bucket check are still not implemented; on an existing bucket they keep answering as before (not implemented). The object features wire them later.
- DeleteBucket and bucket policies are out of scope, so a bucket, once created, exists until its data is removed by the operator.
