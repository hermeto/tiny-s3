# Contract: PutObject — `PUT /<bucket>/<key>`

Routing (003) and the bucket gate (006) are unchanged. Every response carries `x-amz-request-id`; errors use the S3 XML error body.

## Request

| Header | Use |
|--------|-----|
| `Content-Length` | Required (411 otherwise); the declared length is authoritative |
| `Content-MD5` | Optional; base64 of the 16-byte MD5 |
| `Content-Type` | Optional; stored (default `binary/octet-stream`) |
| `x-amz-meta-<name>` | Optional; stored as metadata `<name>` (lowercase) |
| `Content-Encoding: aws-chunked`, `x-amz-content-sha256: STREAMING-*` | Rejected with 501 in this feature |
| Signature, checksum and other headers | Ignored |

## Responses, in check order

| Condition | Status | Code |
|-----------|--------|------|
| Invalid bucket name / missing bucket | 400 / 404 | `InvalidBucketName` / `NoSuchBucket` |
| aws-chunked | 501 | `NotImplemented` |
| No `Content-Length` | 411 | `MissingContentLength` |
| `Content-MD5` not base64 of 16 bytes | 400 | `InvalidDigest` |
| Metadata over 2048 bytes | 400 | `MetadataTooLarge` |
| Key over 1024 bytes | 400 | `KeyTooLongError` |
| Length over 5 GiB | 400 | `EntityTooLarge` |
| Body ends early / client gone | 400 | `IncompleteBody` |
| MD5 mismatch | 400 | `BadDigest` |
| Storage failure | 500 | `InternalError` (logged as `storage_error`) |
| Stored | 200 | `ETag: "<hex md5>"`, empty body |

| Code | Message |
|------|---------|
| `MissingContentLength` | You must provide the Content-Length HTTP header. |
| `InvalidDigest` | The Content-MD5 you specified is not valid. |
| `MetadataTooLarge` | Your metadata headers exceed the maximum allowed metadata size. |
| `KeyTooLongError` | Your key is too long. |
| `EntityTooLarge` | Your proposed upload exceeds the maximum allowed size. |
| `IncompleteBody` | You did not provide the number of bytes specified by the Content-Length HTTP header. |
| `BadDigest` | The Content-MD5 you specified did not match what we received. |
| `InvalidArgument` | Invalid Argument |

## Storage

```go
type PutOptions struct {
    ContentMD5  []byte
    ContentType string
    Metadata    map[string]string
}
func (s *Store) Put(ctx context.Context, bucket, key string, body io.Reader, size int64, opts PutOptions) (Info, error)
```

`Info` gains `ContentType` and `Metadata`; `Head` and `Get` return them.
