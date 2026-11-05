# Research: GetObject and HeadObject

Every decision took the recommended option, as authorized by the feature owner.

## R1 Store interface

- **Decision**: `s3api.ObjectStore` gains `Get(bucket, key) (storage.Info, storage.Reader, error)` and `Head(bucket, key) (storage.Info, error)`, the 005 engine methods. `GetObjectOp` and `HeadObjectOp` are wired in `app.Main` beside `PutObjectOp`.
- **Rationale**: The engine already returns metadata plus an immutable reader opened under the key lock (005 R4), which gives FR-007 for free.

## R2 Response headers

- **Decision**: One function writes `ETag: "<md5>"`, `Content-Length`, `Content-Type`, `Last-Modified` (`http.TimeFormat`, i.e. RFC 1123 in GMT, truncated to the second), `Accept-Ranges: bytes`, and `x-amz-meta-<name>` per metadata entry, for GET, HEAD and (ETag and Last-Modified only) 304.

## R3 Conditional requests

- **Decision**: `evaluateConditions(h http.Header, info) (status int)` returns 200, 304 or 412, in the clarified order: `If-Match` (412 unless an entry matches; `*` matches any existing object) else `If-Unmodified-Since` (412 if modified after the date); then `If-None-Match` (304 if an entry matches; `*` matches) else `If-Modified-Since` (304 if not modified after the date). ETag lists are split on commas; entries may be quoted, unquoted or weak (`W/`), all compared by their hex value. Dates parse with `http.ParseTime` (RFC 1123, RFC 850, ANSI C); an unparseable date is ignored. The last-modified time is truncated to whole seconds.
- **Rationale**: This is RFC 9110's order, and S3's documented special cases (If-Match true + If-Unmodified-Since false → 200; If-None-Match false + If-Modified-Since true → 304) are exactly what it gives.
- **Alternatives considered**: `http.ServeContent` (handles conditionals and ranges, but its range and error behavior differ from S3, its errors are not S3 XML, and feature 010 needs S3's multi-range rule).

## R4 Streaming and failure after the status

- **Decision**: GET copies the reader to the response with `io.CopyBuffer` and a 32 KiB buffer. A read error is logged as `storage_error` and, like a write error (client gone), ends with `panic(http.ErrAbortHandler)`, which the 003 handler re-panics so net/http aborts the connection; the client sees a failed transfer, never a short success. The reader is always closed.
- **Rationale**: Once the status and `Content-Length` are sent, aborting is the only honest signal (FR-008).

## R5 Errors

| Condition | Answer |
|-----------|--------|
| `storage.ErrNoSuchKey` | 404 `NoSuchKey`, "The specified key does not exist." |
| Precondition failed | 412 `PreconditionFailed`, "At least one of the pre-conditions you specified did not hold" |
| Not modified | 304, no body |
| Corrupt record, I/O error before the status | 500 `InternalError` (shared mapping) |

## R6 Proving memory and consistency

- **Decision**: The 007 2 GiB e2e test also downloads the object, checks its MD5 and requires the peak RSS to stay below 64 MiB. Consistency: a handler test on a real store starts a GET of a multi-buffer object, reads part of the body, overwrites the key with a different body and deletes it through the engine, then finishes the read and compares with the original. An e2e test does the same with an HTTP overwrite; the HTTP delete arrives with feature 011, which adds it to that e2e test.
