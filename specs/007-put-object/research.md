# Research: PutObject

Every decision took the recommended option, as authorized by the feature owner.

## R1 What the aws CLI sends

- **Finding**: A capture of `aws s3api put-object` (CLI 2.31.11) against a plain HTTP endpoint shows a plain body with `Content-Length`, `Expect: 100-continue`, a hex `X-Amz-Content-SHA256`, and `x-amz-checksum-crc64nvme` plus `x-amz-sdk-checksum-algorithm: CRC64NVME` headers. It does not use aws-chunked over plain HTTP. With `AWS_REQUEST_CHECKSUM_CALCULATION=when_required` the checksum headers disappear.
- **Decision**: The default CLI works with this feature; the checksum headers are ignored here and verified by feature 008, which owns checksums (spec Assumptions).

## R2 Storage API change

- **Decision**: `Store.Put(ctx, bucket, key, body, size, opts PutOptions)` with `PutOptions{ContentMD5 []byte; ContentType string; Metadata map[string]string}`. The record gains `content_type` and `metadata` (both `omitempty`), keeping format version 1; `Info` gains `ContentType` and `Metadata`. A record without `content_type` reads as `binary/octet-stream` (clarification 5).
- **Rationale**: One options struct keeps the call sites readable and lets feature 008 add checksum options without another signature change. 005 designed the record for this addition.
- **Alternatives considered**: A second `PutWithMetadata` (two write paths to keep in sync); a record version 2 (needs a migration for no reason).

## R3 Early disconnect

- **Decision**: `copyBody` maps `io.ErrUnexpectedEOF` from the body to `ErrIncompleteBody`; the S3 layer also maps `context.Canceled` (the request context ends when the client goes away) to `IncompleteBody`. Both fail before the commit, so the 005 cleanup removes the temp file.
- **Rationale**: net/http returns `io.ErrUnexpectedEOF` when a body with a declared length ends early; the spec asks for `IncompleteBody`.

## R4 Request checks and order

- **Decision**, in this order, all before the first body read (so `Expect: 100-continue` clients never send the body): bucket gate (006) → aws-chunked → missing length (`r.ContentLength < 0`) → `Content-MD5` decode (base64, 16 bytes) → user metadata (collect `X-Amz-Meta-*`, lowercase names, join repeats with `,`, limit 2048 bytes) → `Store.Put`, which validates key length and size before touching the body.
- **Rationale**: Every rejection is cheap and leaves the body unread; net/http then closes or drains the connection.

## R5 Operation wiring

- **Decision**: `s3api.PutObjectOp(store ObjectStore, log) Op`, assigned in `app.Main` next to `BucketOps`. Storage errors map through one shared function (generalizing 006's bucket error mapping) to S3 errors; unknown errors answer 500 `InternalError` and are logged as `storage_error` with the request ID, operation, bucket and key. 006's `bucket_error` log becomes `storage_error`.
- **Rationale**: Each object feature adds one exported Op constructor; the mapping stays in one place.

## R6 Error codes

| Storage error / condition | S3 answer |
|---------------------------|-----------|
| aws-chunked | 501 `NotImplemented` |
| no length | 411 `MissingContentLength` |
| bad `Content-MD5` | 400 `InvalidDigest` |
| metadata > 2048 bytes | 400 `MetadataTooLarge` |
| `ErrKeyTooLong` | 400 `KeyTooLongError` |
| `ErrEntityTooLarge` | 400 `EntityTooLarge` |
| `ErrBadDigest` | 400 `BadDigest` |
| `ErrIncompleteBody`, `context.Canceled` | 400 `IncompleteBody` |
| `ErrInvalidArgument` | 400 `InvalidArgument` |
| other | 500 `InternalError` |

## R7 Proving flat memory at 2 GiB

- **Decision**: An e2e test streams a generated 2 GiB body to the real s3d binary with a Go HTTP client (computing MD5 on the client), then reads the server's peak resident memory (`VmHWM` in `/proc/<pid>/status`) and requires it below 64 MiB, plus the ETag match. The 005 unit test (64 MiB Put < 4 MiB allocated) stays the fast guard.
- **Rationale**: Measures the real process at the size the spec names. Linux-only, which holds in the e2e container.
- **Alternatives considered**: A 2 GiB file through the aws CLI (writes 2 GiB to disk twice, slower, adds nothing the Go client does not prove).
