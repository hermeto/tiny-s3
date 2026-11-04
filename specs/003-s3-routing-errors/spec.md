# Feature Specification: S3 Request Routing and Error Responses

**Feature Branch**: `003-s3-routing-errors`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "S3 request routing and error responses. Any S3 client sending path-style requests (/{bucket} and /{bucket}/{key}) reaches the right operation handler based on HTTP method and query parameters, and every failure comes back in the S3 XML error format so clients like the aws CLI show a meaningful message. Acceptance criteria: Requests are dispatched to distinct handlers for ListBuckets (GET /), CreateBucket (PUT /b), HeadBucket (HEAD /b), ListObjectsV2 (GET /b?list-type=2), PutObject (PUT /b/k), GetObject (GET /b/k), HeadObject (HEAD /b/k) and DeleteObject (DELETE /b/k). Unimplemented operations return 501 NotImplemented. Unsupported methods return 405 MethodNotAllowed. Every response carries an x-amz-request-id header. Error bodies are <Error><Code/><Message/><Resource/><RequestId/></Error> with Content-Type application/xml. HEAD errors have no body. Keys may contain slashes, spaces, unicode and URL-encoded characters, and are decoded exactly once."

## Clarifications

### Session 2026-11-04

- Q: When a request matches an in-scope operation but also carries an S3 subresource (for example `GET /b?list-type=2&acl`), which wins? → A: The subresource wins; the request gets `501 NotImplemented` and no in-scope handler is called.
- Q: What happens when a bucket or key decodes to bytes that are not valid UTF-8 (for example `/b/%FF`)? → A: Routing rejects it with `400 InvalidURI`, so no operation handler ever sees a non-UTF-8 name.
- Q: Is the bucket segment percent-decoded like the key? → A: Yes, exactly once, by the same rule as the key; validating the decoded name is left to the bucket operations.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Each S3 request reaches the right operation (Priority: P1)

An S3 client (the aws CLI, an SDK, curl) sends a path-style request. The server works out which S3 operation the request means from the HTTP method, the path shape (service, bucket, or object) and the query parameters, and hands it to that operation's handler together with the bucket name and the object key. The eight in-scope operations each have their own handler. Later features fill those handlers in one by one without touching the routing.

**Why this priority**: Every later S3 operation depends on it. Without a routing layer, each operation feature would have to re-derive the operation from raw requests, and a mistake (for example, treating a CopyObject as a PutObject with an empty body) would corrupt data.

**Independent Test**: Plug a recording handler into each of the eight operation slots, send one request per operation plus look-alike requests (other subresources, other headers), and check that exactly the expected slot was called with the expected bucket and key.

**Acceptance Scenarios**:

1. **Given** the server is running, **When** a client sends `GET /`, **Then** the ListBuckets handler is called.
2. **Given** the server is running, **When** a client sends `PUT /b`, `HEAD /b` or `GET /b?list-type=2`, **Then** the CreateBucket, HeadBucket or ListObjectsV2 handler is called with bucket `b`.
3. **Given** the server is running, **When** a client sends `PUT /b/k`, `GET /b/k`, `HEAD /b/k` or `DELETE /b/k`, **Then** the PutObject, GetObject, HeadObject or DeleteObject handler is called with bucket `b` and key `k`.
4. **Given** a request with a trailing slash on the bucket (`PUT /b/`), **When** it is routed, **Then** it is treated as the bucket-level request (`PUT /b`), not as an object with an empty key.
5. **Given** a ListObjectsV2 request with its normal parameters (`prefix`, `delimiter`, `max-keys`, `continuation-token`, `start-after`, `fetch-owner`, `encoding-type`), **When** it is routed, **Then** it still reaches ListObjectsV2; those parameters do not change the operation.
6. **Given** a GetObject request with response-override parameters (`response-content-type` and the other `response-*` parameters), **When** it is routed, **Then** it still reaches GetObject.

---

### User Story 2 - Every failure is a readable S3 error (Priority: P1)

When a request cannot be served, whether the operation is not built yet, the method makes no sense for the resource, or something unexpected breaks inside the server, the client gets the HTTP status S3 would use and an S3 XML error body. The aws CLI then prints a line such as `An error occurred (NotImplemented) when calling the ListObjects operation: A header or method you provided implies functionality that is not implemented.` instead of a parse error or a bare status code.

**Why this priority**: It is the other half of the acceptance criteria and the constitution requires it (Principle V). A client that cannot parse an error hides the cause from the user.

**Independent Test**: Send requests for known-but-unimplemented operations, unsupported methods and a handler that panics; check the status, `Content-Type`, the four XML elements and that the `RequestId` element equals the `x-amz-request-id` header. Repeat each with `HEAD` and check that the body is empty.

**Acceptance Scenarios**:

1. **Given** a request for an S3 operation outside this server's scope (for example `GET /b` without `list-type=2`, which is ListObjects v1, `GET /b?acl`, `DELETE /b`, `POST /b?delete`, `PUT /b/k?tagging`, `GET /b/k?uploadId=x`), **When** it is routed, **Then** the response is `501` with code `NotImplemented`.
2. **Given** a `PUT /b/k` request carrying an `x-amz-copy-source` header (CopyObject), **When** it is routed, **Then** the response is `501 NotImplemented` and the PutObject handler is never called.
3. **Given** a method S3 does not support (for example `PATCH`, `OPTIONS`, `TRACE`, or a made-up method) on any path, or a supported method with no meaning on the resource (`PUT /`, `DELETE /`, `POST /`, `HEAD /`), **When** it is routed, **Then** the response is `405` with code `MethodNotAllowed`.
4. **Given** any error response, **When** the client reads it, **Then** `Content-Type` is `application/xml` and the body is an XML document with root `Error` and the children `Code`, `Message`, `Resource` and `RequestId`, in that order.
5. **Given** any error response to a `HEAD` request, **When** the client reads it, **Then** it gets the status and headers (including `x-amz-request-id`) and an empty body.
6. **Given** an operation handler that panics, **When** the request is served, **Then** the client gets `500` with code `InternalError` (unless the handler had already started its response), and the server keeps serving other requests.
7. **Given** a `HEAD` request that routes to a 501 or 405, **When** the response is sent, **Then** `x-amz-request-id` is present even though there is no body.

---

### User Story 3 - Every response can be traced by request ID (Priority: P2)

Every response the server sends, success or failure, carries an `x-amz-request-id` header with a value unique to that request. For errors, the same value is in the `RequestId` element of the body. When a user reports a failure, the operator can match the ID from the client output to the request.

**Why this priority**: Clients and SDKs surface this ID in error messages and debug logs. It is cheap and needed by every operation, but routing and error format come first.

**Independent Test**: Send many requests of every kind (routed to a handler that succeeds, 501, 405, 500, HEAD), collect the header, and check that it is always present, always in the expected format, never repeats, and equals the body's `RequestId` on errors.

**Acceptance Scenarios**:

1. **Given** a request routed to an operation handler that succeeds, **When** the response is sent, **Then** it carries `x-amz-request-id`.
2. **Given** an operation handler that writes an S3 error, **When** the response is sent, **Then** the error body's `RequestId` equals the response's `x-amz-request-id`; the request has one ID, not one per write.
3. **Given** two requests, **When** both are served, **Then** their request IDs differ.

---

### User Story 4 - Keys with any characters reach the handler intact (Priority: P1)

A user stores objects whose keys contain slashes (`photos/2026/a.jpg`), spaces, unicode (`relatório/ação.txt`), characters that clients percent-encode (`a+b`, `100%`, `#`, `?`), dot segments and doubled slashes. The handler receives exactly the key the user typed, decoded exactly once from the request path.

**Why this priority**: A key decoded twice, or cleaned like a filesystem path, points at a different object than the user named: data silently lands under the wrong name or cannot be found again.

**Independent Test**: For a table of keys, encode each the way the aws CLI and SDKs do, send it, and check that the handler received the original key byte for byte.

**Acceptance Scenarios**:

1. **Given** a key with slashes (`a/b/c`), **When** it is routed, **Then** the handler receives bucket `b` and key `a/b/c`; only the first path segment is the bucket.
2. **Given** a key with spaces sent as `%20`, **When** it is routed, **Then** the key contains spaces.
3. **Given** a key with unicode sent as percent-encoded UTF-8, **When** it is routed, **Then** the key is the original unicode string.
4. **Given** a key containing a literal `%` sent as `%25` (for example `100%25` or `%2541`), **When** it is routed, **Then** the key is `100%` or `%41`; it is not decoded a second time.
5. **Given** a key containing `+` in the path, **When** it is routed, **Then** the key keeps the `+`; it is not turned into a space.
6. **Given** a key with an encoded slash (`a%2Fb`), **When** it is routed, **Then** the key is `a/b`, the same object as `a/b` sent unencoded.
7. **Given** keys with a trailing slash (`dir/`), doubled slashes (`a//b`) or dot segments (`./a`, `a/../b`), **When** they are routed, **Then** they reach the handler unchanged, with no redirect and no path cleaning.
8. **Given** any error for an object request, **When** the error body is written, **Then** `Resource` is `/bucket/key` with the decoded key, XML-escaped.

---

### Edge Cases

- A request target that is not valid percent-encoding (for example `/b/%zz`) is rejected by the HTTP layer with a plain `400 Bad Request` before routing runs, so it is not in S3 XML format. Accepted; real clients never send it (see Assumptions).
- A query string with malformed pairs (for example `?a=%zz&acl`): the malformed pairs are ignored for routing and the well-formed ones still count (here `acl`, so `501`); the operation handler gets the raw query to reject if it needs to.
- A query parameter that is not an S3 subresource and not a known operation parameter (for example `?foo=bar`): ignored by routing.
- Subresource names are matched exactly and case-sensitively, as S3 does: `?ACL` is not `?acl`.
- `list-type` with a value other than `2`: treated as an unimplemented list variant, `501 NotImplemented`.
- An empty bucket segment with a key (`//k`): there is no bucket, so the path is malformed and answered `400 InvalidURI` (FR-006).
- Absolute-form request targets (`GET http://host/b/k HTTP/1.1`) route on their path exactly as origin-form requests do.
- Virtual-hosted-style requests (`Host: b.localhost`): not recognized; routed as path-style on the path alone (see Assumptions).
- A handler that panics after writing part of its response: the status cannot be changed; the connection is aborted so the client sees a failed transfer instead of a truncated success.
- An unsupported method with a large request body: the server answers without reading the body beyond what the HTTP layer drains by default.
- An in-scope request that also carries a subresource (`GET /b?list-type=2&acl`): the subresource wins, `501 NotImplemented`.
- A bucket or key that decodes to invalid UTF-8 (`/b/%FF`): `400 InvalidURI`.
- `HEAD` on a path whose `GET` is unimplemented (for example `HEAD /b?acl`): `501` with no body.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The server MUST interpret every request as path-style: the path `/` is the service; the first path segment is the bucket name; everything after the first `/` that follows the bucket is the object key. A bucket path with a trailing slash and nothing after it (`/b/`) MUST be treated as the bucket path.
- **FR-002**: The server MUST dispatch to eight distinct operation handlers: ListBuckets (`GET /`), CreateBucket (`PUT /{bucket}`), HeadBucket (`HEAD /{bucket}`), ListObjectsV2 (`GET /{bucket}?list-type=2`), PutObject (`PUT /{bucket}/{key}`), GetObject (`GET /{bucket}/{key}`), HeadObject (`HEAD /{bucket}/{key}`) and DeleteObject (`DELETE /{bucket}/{key}`). Each handler MUST receive the bucket name and, for object operations, the key.
- **FR-003**: A request MUST reach one of the eight handlers only when it carries no S3 subresource query parameter and no header that selects a different S3 operation (`x-amz-copy-source` on PUT selects CopyObject). A subresource takes precedence over everything else, including `list-type=2`. Parameters that belong to the operation itself (ListObjectsV2 listing parameters, GetObject `response-*` overrides) MUST NOT prevent dispatch.
- **FR-004**: A request that names an S3 operation the server does not implement, for example a known subresource on a bucket or object, ListObjects v1, a `list-type` other than `2`, DeleteBucket, any `POST`, or CopyObject, MUST get `501 Not Implemented` with code `NotImplemented` and the message `A header or method you provided implies functionality that is not implemented.`
- **FR-005**: A request whose method is not one of `GET`, `HEAD`, `PUT`, `POST`, `DELETE`, or whose method has no S3 meaning on the resource (`PUT`, `POST`, `DELETE` and `HEAD` on `/`), MUST get `405 Method Not Allowed` with code `MethodNotAllowed` and the message `The specified method is not allowed against this resource.`
- **FR-006**: A path with an empty bucket segment followed by more segments (`//k`), or whose decoded bucket or key is not valid UTF-8, MUST get `400 Bad Request` with code `InvalidURI` and the message `Couldn't parse the specified URI.`
- **FR-007**: Until a later feature implements it, each of the eight operation handlers MUST answer `501 NotImplemented`, exactly as FR-004.
- **FR-008**: Every response, success or error, from routing or from an operation handler, MUST carry an `x-amz-request-id` header. Its value MUST be generated once per request, MUST be 16 uppercase hexadecimal characters, and MUST differ between requests.
- **FR-009**: Every error response MUST have `Content-Type: application/xml` and a body made of the XML declaration followed by `<Error>` with the children `Code`, `Message`, `Resource` and `RequestId`, in that order, with all text XML-escaped. `RequestId` MUST equal the response's `x-amz-request-id`. `Resource` MUST be the decoded request path.
- **FR-010**: Error responses to `HEAD` requests MUST carry the status and headers and MUST have an empty body.
- **FR-011**: Operation handlers MUST have one shared way to write S3 errors that reuses the request's ID and applies FR-009 and FR-010, so no handler renders error XML itself.
- **FR-012**: The bucket name and the object key MUST be the matching parts of the request path, each percent-decoded exactly once. `+` MUST stay `+`. An encoded slash (`%2F`) MUST decode to `/`. The path MUST NOT be cleaned: trailing slashes, doubled slashes and `.` / `..` segments are part of the key, and no redirect is issued.
- **FR-013**: A panic in an operation handler MUST be recovered: if the response has not started, the client MUST get `500 Internal Server Error` with code `InternalError` and the message `We encountered an internal error. Please try again.`; if it has started, the connection MUST be aborted. The panic MUST be logged with its request ID, and the server MUST keep serving.
- **FR-014**: The README design section MUST record the trade-offs made by this feature (which subresources are recognized, CopyObject detection, the 405/501 split, the plain-text 400 for malformed request targets, path-style only), per the constitution's Development Workflow.

### Key Entities

- **Request target**: the parsed path-style address of a request: kind (service, bucket or object), bucket name and object key.
- **Operation**: the S3 action a request names, derived from method, target kind, subresource query parameters and operation-selecting headers. One of the eight in-scope operations, or "not implemented", or "method not allowed".
- **Request ID**: a per-request identifier carried on every response and inside every error body.
- **S3 error**: an HTTP status plus `Code`, `Message`, `Resource` and `RequestId`, rendered as S3 XML.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the eight in-scope operations reach their own handler, and 0 look-alike requests (other subresources, CopyObject, ListObjects v1) reach any of the eight handlers, across the routing test table.
- **SC-002**: 100% of error responses across all tested paths, methods and failure kinds parse as S3 XML errors with the four required elements and a `RequestId` matching the header; 100% of `HEAD` error responses have an empty body.
- **SC-003**: The aws CLI v2, pointed at the server, prints `An error occurred (NotImplemented) when calling the <Operation> operation: ...` for every in-scope operation except the two `HEAD` operations, which have no body by design and print `An error occurred (501) when calling the <Operation> operation: Not Implemented`; no command prints a parsing error.
- **SC-004**: Across at least 1,000 requests of every kind, every response carries `x-amz-request-id` and no ID repeats.
- **SC-005**: For a table of at least 20 keys covering slashes, spaces, unicode, `%`, `+`, `#`, `?`, encoded slashes, trailing and doubled slashes and dot segments, 100% reach the handler byte-identical to the original key.
- **SC-006**: A handler panic produces a `500 InternalError` response and the next request is served normally.

## Assumptions

- Only path-style addressing is supported. Virtual-hosted-style requests are out of scope; clients must be configured for path-style (the aws CLI does this automatically for a custom endpoint with `addressing_style = path`, and for endpoints that are an IP address or `localhost`).
- Authentication and SigV4 verification are out of scope (constitution), so `Authorization` headers and presigned-URL query parameters (`X-Amz-*`) are ignored by routing and never cause a 501.
- Bucket name validation (`InvalidBucketName`), key length limits (`KeyTooLongError`) and existence checks (`NoSuchBucket`, `NoSuchKey`) belong to the operation features; routing only splits the path.
- The set of recognized subresource query parameters is the list S3 documents for bucket and object subresources (for example `acl`, `tagging`, `uploads`, `uploadId`, `partNumber`, `versionId`, `versions`, `versioning`, `location`, `policy`, `cors`, `lifecycle`, `website`, `logging`, `notification`, `replication`, `encryption`, `delete`, `restore`, `select`, `attributes`, `retention`, `legal-hold`, `object-lock`, `torrent`); the exact list is fixed in planning. Versioning is out of scope, so `versionId` selects an unimplemented operation.
- Malformed request targets (invalid percent-encoding in the path) are rejected by the Go HTTP layer with a plain-text `400` before any handler runs. Making that an S3 XML error would need a custom HTTP parser, which the constitution's standard-library-first principle argues against for a case real clients do not produce.
- No aws CLI end-to-end test is added in this feature: none of the eight operations is implemented yet, and the constitution's real-client rule applies to implemented operations. SC-003 is verified manually through the quickstart; the first operation feature adds the aws CLI e2e harness.
- Access logging per request is out of scope; the request ID is logged only for recovered panics.
- Dependency: builds on `internal/s3api` from spec 001 (the error writer and the placeholder handler), which this feature replaces.
