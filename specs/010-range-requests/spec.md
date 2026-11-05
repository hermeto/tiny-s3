# Feature Specification: Range Requests

**Feature Branch**: `010-range-requests`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Range requests on GetObject, matching S3 behavior, so clients can resume downloads and read parts of large objects. Acceptance criteria: aws s3api get-object --range bytes=0-99 returns 206 with exactly 100 bytes and Content-Range: bytes 0-99/<size>. bytes=100- returns from byte 100 to the end. bytes=-50 returns the last 50 bytes. A range starting beyond the object size returns 416 InvalidRange with Content-Range: bytes */<size> and an S3 XML body. An end beyond the size is clamped. A request with multiple ranges returns the whole object with 200, as S3 does. Malformed Range headers are ignored and return the whole object. If-Range with a stale ETag returns the whole object."

## Clarifications

### Session 2026-11-04

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: Does HeadObject honor `Range`? → A: Yes, as S3 does: the same status and headers as the matching GET (206 with `Content-Range` and the range's `Content-Length`, or 416), without a body.
- Q: How strict is the `Range` syntax? → A: The unit `bytes` is case-insensitive and spaces around the whole value and around the `-` are tolerated; anything else that is not one well-formed range (letters, a missing number on both sides, first greater than last, a number that does not fit in 63 bits) is malformed and ignored.
- Q: What does the 416 body contain? → A: The standard S3 error body (Code `InvalidRange`, Message "The requested range is not satisfiable", Resource, RequestId) plus the `Content-Range: bytes */size` header.
- Q: When does `If-Range` match? → A: A strong ETag equal to the object's (quoted or not), or an HTTP date equal to the object's last-modified time in whole seconds. A weak ETag or an unparseable value never matches, so the whole object is returned.
- Q: Is `If-Range` evaluated before or after the 009 conditional headers? → A: After: `If-Match`/`If-None-Match`/date conditions decide 412/304 first; only a request that passes them is subject to `If-Range` and `Range`.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Read part of an object (Priority: P1)

As an S3 client user, I download only a byte range of an object, to resume an interrupted download or read a slice of a large file.

**Why this priority**: Resumable and parallel downloads depend on byte ranges; it is the core of this feature.

**Independent Test**: Upload a known file and request the three range forms with the S3 command-line client, comparing each result with the matching slice of the file.

**Acceptance Scenarios**:

1. **Given** an object of size N ≥ 100, **When** the client requests `bytes=0-99`, **Then** the response is 206 with exactly the first 100 bytes and `Content-Range: bytes 0-99/N`.
2. **Given** an object of size N > 100, **When** the client requests `bytes=100-`, **Then** the response is 206 with bytes 100 to N−1 and `Content-Range: bytes 100-(N−1)/N`.
3. **Given** an object of size N ≥ 50, **When** the client requests `bytes=-50`, **Then** the response is 206 with the last 50 bytes.
4. **Given** an object of size N > 10, **When** the client requests `bytes=10-M` with M ≥ N, **Then** the end is clamped to N−1 (`Content-Range: bytes 10-(N−1)/N`).
5. **Given** an object of size N, **When** the client requests `bytes=-M` with M > N, **Then** the response is 206 with the whole object (`bytes 0-(N−1)/N`).

---

### User Story 2 - Unsatisfiable ranges (Priority: P1)

As an S3 client user, a range that starts past the end of the object fails clearly, so my resume logic knows the download is already complete or the object changed.

**Why this priority**: Resume logic relies on this answer to stop.

**Independent Test**: Request a range starting at the object size and beyond.

**Acceptance Scenarios**:

1. **Given** an object of size N, **When** the client requests `bytes=N-`, **Then** the response is 416 `InvalidRange` with `Content-Range: bytes */N` and an S3 XML error body.
2. **Given** an empty object, **When** the client requests any range, **Then** the response is 416 `InvalidRange` with `Content-Range: bytes */0`.

---

### User Story 3 - Ranges S3 ignores (Priority: P2)

As an S3 client user, requests S3 would answer with the whole object get the whole object here too, so behavior matches what my client expects from S3.

**Why this priority**: Compatibility details that matter less than the main range forms.

**Independent Test**: Send multiple ranges, malformed headers and a stale `If-Range`, and check that each returns the whole object with 200.

**Acceptance Scenarios**:

1. **Given** a `Range` with more than one range (`bytes=0-1,5-6`), **When** it is sent, **Then** the response is 200 with the whole object.
2. **Given** a malformed `Range` (`bytes=abc`, `items=0-1`, `bytes=5-3`), **When** it is sent, **Then** the response is 200 with the whole object.
3. **Given** `If-Range` with an ETag other than the object's, **When** a range is requested, **Then** the response is 200 with the whole object; with the current ETag, the range is served.

### Edge Cases

- A range covering the whole object (`bytes=0-`) answers 206, not 200.
- `bytes=-0` is unsatisfiable (416).
- Conditional headers from feature 009 are evaluated first: a 412 or 304 wins over the range.
- A failure while reading the range after the status aborts the connection, as for full downloads.
- Large ranges stream with the same fixed memory as full downloads.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A GetObject request with one satisfiable byte range MUST answer 206 with exactly those bytes, `Content-Range: bytes first-last/size` and `Content-Length` equal to the range length, plus the headers of feature 009.
- **FR-002**: The forms `first-last`, `first-` and `-suffix` MUST be supported; an end beyond the object is clamped; a suffix longer than the object returns the whole object as a range.
- **FR-003**: A range whose start is at or beyond the object size, a zero-length suffix, or any range on an empty object MUST answer 416 `InvalidRange` with `Content-Range: bytes */size` and an S3 XML body.
- **FR-004**: A `Range` with multiple ranges, or one that is malformed, MUST be ignored and the whole object returned with 200.
- **FR-005**: `If-Range` MUST make the range apply only when it matches the object (its ETag, or its exact last-modified date); otherwise the whole object is returned with 200.
- **FR-006**: Range responses MUST stream with bounded memory, and failures after the status MUST abort the connection.
- **FR-007**: The range forms and the unsatisfiable case MUST be exercised end to end by the real S3 command-line client (constitution Principle V).
- **FR-008**: HeadObject MUST answer a `Range` with the same status and headers as GetObject, without a body.
- **FR-009**: `Range` parsing MUST follow the syntax rules in Clarifications; `If-Range` MUST follow the matching rules there and be evaluated after the 009 conditions.

### Key Entities

- **Byte range**: first and last byte positions (inclusive) within an object of a known size.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of range downloads return bytes identical to the matching slice of the source file, for the three forms and clamped ends.
- **SC-002**: Every unsatisfiable, multi-range, malformed and stale `If-Range` case in the acceptance scenarios returns the specified status.
- **SC-003**: A 1 GiB range of a 2 GiB object streams with server peak memory below 64 MiB.

## Assumptions

- Ranges apply to GetObject and, without a body, to HeadObject (clarification 1).
- Ranges never return multipart/byteranges bodies; S3 does not.
