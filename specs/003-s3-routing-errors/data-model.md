# Data Model: S3 Request Routing and Error Responses

No persisted data. These are the in-memory types of `internal/s3api` ([research.md](research.md) R6 to R9).

## Target

The path-style address of a request.

| Field | Type | Rule |
|-------|------|------|
| `Bucket` | string | First path segment, decoded once. Empty only for the service (`/`). Valid UTF-8. |
| `Key` | string | Everything after the `/` that follows the bucket, decoded once, never cleaned. Empty for bucket-level requests. Valid UTF-8. |

Kind is derived: service (`Bucket == ""`), bucket (`Key == ""`), object (both set).

## Op and Ops

`Op` is `func(http.ResponseWriter, *http.Request, Target)`. `Ops` has one `Op` field per in-scope operation:
`ListBuckets`, `CreateBucket`, `HeadBucket`, `ListObjectsV2`, `PutObject`, `GetObject`, `HeadObject`,
`DeleteObject`. A nil field answers `ErrNotImplemented`.

## Route decision

The router maps a request to exactly one outcome:

| Outcome | When |
|---------|------|
| one of the eight `Ops` | [contracts/http.md](contracts/http.md) routing table |
| `ErrMethodNotAllowed` | method outside `GET HEAD PUT POST DELETE`, or a non-`GET` on `/` |
| `ErrInvalidURI` | empty bucket on a path other than `/`, or non-UTF-8 bucket or key |
| `ErrNotImplemented` | subresource present, ListObjects v1 or other `list-type`, DeleteBucket, `POST`, CopyObject |

## APIError

| Field | Type | Example |
|-------|------|---------|
| `Status` | int | `501` |
| `Code` | string | `NotImplemented` |
| `Message` | string | `A header or method you provided implies functionality that is not implemented.` |

Values defined here: `ErrNotImplemented`, `ErrMethodNotAllowed`, `ErrInvalidURI`, `ErrInternalError`. Later
features add their own.

## Request ID

16 uppercase hex characters from 8 random bytes. Created once per request by the outer handler, sent in
`x-amz-request-id`, stored in the request context, read by `RequestID(ctx)` and by `WriteError`.

## Lifecycle of a request

1. Generate the request ID, set the header, store it in the context, wrap the writer, defer recovery.
2. Route (table in the contract). Write an error, or call the `Op`.
3. On panic: log `handler_panic`; `500 InternalError` if nothing was written, connection abort otherwise.
