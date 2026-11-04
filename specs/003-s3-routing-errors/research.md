# Research: S3 Request Routing and Error Responses

No `NEEDS CLARIFICATION` remained in the Technical Context. Each entry records a design decision.

## R1. Custom router, not `http.ServeMux`

- **Decision**: `internal/s3api` dispatches with its own function over `r.Method`, `r.URL.Path`, the parsed query and
  one header. No `http.ServeMux`.
- **Rationale**: `ServeMux` cleans paths and redirects (`a//b`, `./a`, `a/../b`, trailing-slash rules), which
  breaks FR-012. Its Go 1.22 patterns match method and path but not query parameters or headers, so the
  subresource and CopyObject rules would still need custom code. Eight operations fit in one `switch`.
- **Alternatives considered**: `ServeMux` with `{key...}` wildcards (path cleaning, no query matching);
  a table of predicates (more indirection than a `switch` for eight cases).

## R2. Decode exactly once: use `r.URL.Path` as is

- **Decision**: bucket and key come from `r.URL.Path`, which `net/http` has already percent-decoded once with
  path rules (`+` stays `+`, `%2F` becomes `/`). The router never calls `url.PathUnescape`.
- **Rationale**: decoding `r.URL.EscapedPath()` ourselves would be the same single decode with more code;
  decoding `r.URL.Path` again is exactly the double-decode bug FR-012 forbids. `net/http.Server` itself does not
  clean paths; only `ServeMux` does. Absolute-form targets set the same `URL.Path`.
- **Consequence**: `a%2Fb` and `a/b` are the same key, which matches S3. A target with invalid percent-encoding
  fails `url.ParseRequestURI` inside `net/http` and gets a plain-text `400` before the handler runs (spec
  Assumptions).

## R3. Path split and validation

- **Decision**: `/` is the service. Otherwise strip the leading `/` and split at the first `/`: bucket before,
  key after. Empty key means bucket-level (`/b` and `/b/`). Empty bucket on any path other than `/` (`//`, `//k`),
  a path not starting with `/` that reaches this step, or a bucket or key that is not valid UTF-8
  (`utf8.ValidString`) is `400 InvalidURI`.
- **Rationale**: matches FR-001, FR-006 and the clarification. Bucket name rules belong to bucket operations.

## R4. Decision order

- **Decision**:
  1. Method not in `GET HEAD PUT POST DELETE` → `405 MethodNotAllowed`. Runs first, so `OPTIONS *` and
     `CONNECT` never reach path parsing.
  2. Parse the target (R3) → `400 InvalidURI` on failure.
  3. Service: `GET` → ListBuckets; any other method → `405`. The query is ignored on the service.
  4. Bucket or object with any subresource key in the query (R5) → `501 NotImplemented`.
  5. Bucket: `GET` with `list-type=2` → ListObjectsV2; `GET` with any other or no `list-type` → `501`;
     `PUT` → CreateBucket; `HEAD` → HeadBucket; `DELETE`, `POST` → `501`.
  6. Object: `PUT` with an `x-amz-copy-source` header → `501`; `PUT` → PutObject; `GET` → GetObject;
     `HEAD` → HeadObject; `DELETE` → DeleteObject; `POST` → `501`.
- **Rationale**: S3 answers `405` for a method with no meaning on a resource and `501` for a real operation the
  server lacks. Every `POST` maps to an S3 operation (PostObject, DeleteObjects, multipart, restore, select),
  so `POST` is `501`, not `405`. Checking subresources before the method switch implements "subresource wins".
- **Alternatives considered**: `405` for `DELETE /b` (DeleteBucket is a real S3 operation, so `501` is what a
  client should see).

## R5. Subresource list

- **Decision**: a fixed set of query keys, matched case-sensitively on presence (any value, including empty):
  `accelerate acl analytics attributes cors delete encryption intelligent-tiering inventory legal-hold
  lifecycle location logging metadataTable metrics notification object-lock ownershipControls partNumber
  policy policyStatus publicAccessBlock replication requestPayment restore retention select session tagging
  torrent uploadId uploads versionId versioning versions website`.
- **Rationale**: these are the bucket and object subresources in the S3 API reference, plus `partNumber` and
  `versionId`, which select behaviour this server does not have (part reads, versioning). Everything else is
  ignored, which is what keeps `x-id=GetObject` (added by current AWS SDKs), `X-Amz-*` presign parameters,
  ListObjectsV2 parameters and `response-*` overrides from blocking dispatch.
- **Alternatives considered**: an allowlist of parameters per operation, rejecting anything unknown (breaks
  every new SDK parameter; S3 itself ignores unknown parameters).
- **Query parsing**: `url.ParseQuery` on `r.URL.RawQuery`; its error is ignored because it still returns every
  well-formed pair (spec edge case).

## R6. Operation handlers as a struct of function fields

- **Decision**:

  ```go
  type Target struct{ Bucket, Key string }
  type Op func(w http.ResponseWriter, r *http.Request, t Target)
  type Ops struct{ ListBuckets, CreateBucket, HeadBucket, ListObjectsV2, PutObject, GetObject, HeadObject, DeleteObject Op }
  func NewHandler(ops Ops, log *slog.Logger) http.Handler
  ```

  A nil field answers `501 NotImplemented` (FR-007). Each later feature sets one field.
- **Rationale**: tests plug a recorder into each field to prove dispatch (US1 independent test). A struct keeps
  every operation named at the call site; an interface with eight methods would force every later feature to
  implement all eight.

## R7. One request ID per request

- **Decision**: the outer handler generates the ID (16 uppercase hex from `crypto/rand`, unchanged from 001),
  sets `x-amz-request-id` on the response headers before any routing, and stores it in the request context.
  `RequestID(ctx)` reads it. `WriteError` uses that ID for the body and re-sets the header from it; without one
  in the context (a handler used outside the router), it generates one.
- **Rationale**: FR-008 and US3 scenario 2: success responses get the header too, and an operation's own error
  body carries the same ID as the header.

## R8. Error values and `WriteError`

- **Decision**: `type APIError struct{ Status int; Code, Message string }` with package values
  `ErrNotImplemented` (501), `ErrMethodNotAllowed` (405), `ErrInvalidURI` (400), `ErrInternalError` (500), using
  S3's exact messages. `WriteError(w, r, e APIError)` sets `Content-Type: application/xml`, writes the status, and
  for anything but `HEAD` writes `xml.Header` plus the `<Error>` element (`Code`, `Message`, `Resource`,
  `RequestId`). `Resource` is `r.URL.Path`.
- **Rationale**: one renderer for FR-009 to FR-011. Later features add their own `APIError` values
  (`NoSuchKey`, ...). `net/http` already drops HEAD bodies, but skipping the write keeps it explicit and avoids
  encoding work.

## R9. Panic recovery

- **Decision**: the outer handler wraps the `ResponseWriter` in a recorder that notes whether the header was
  written, and defers a `recover`. On a panic other than `http.ErrAbortHandler`, it logs `handler_panic` with
  `request_id`, `method`, `path` and `panic` (via `log/slog`). If nothing was written, it sends
  `ErrInternalError`; otherwise it re-panics with `http.ErrAbortHandler`, which `net/http` turns into a silent
  connection abort. `http.ErrAbortHandler` itself is re-panicked unchanged and not logged.
- **Rationale**: FR-013. Without recovery, `net/http` already keeps serving but sends nothing to the client and
  logs to the standard logger, not as JSON.
- **Wrapper**: implements `Unwrap() http.ResponseWriter`, so `http.ResponseController` (flush, deadlines) still
  reaches the real writer for the streaming operations. It does not forward `io.ReaderFrom`; GetObject may add
  that if sendfile matters.
- **Logger wiring**: `app.run` builds the logger, so it now takes `func(*slog.Logger) http.Handler` instead of a
  ready handler; `Main` passes `func(l) { return s3api.NewHandler(s3api.Ops{}, l) }`.

## R10. Tests

- **Decision**: table-driven `httptest` tests in `internal/s3api` for routing (recorder in every `Ops` field),
  errors, request IDs and keys; one e2e test in `e2e/` against the built binary over plain `net/http`, checking a
  501, a 405, a HEAD with no body and the request-ID header, which proves the wiring in `app`.
- **Rationale**: 100% coverage of `internal/` (Principle IV). No aws CLI e2e yet: no operation is implemented
  (spec Assumptions); quickstart covers SC-003 manually.
