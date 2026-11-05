# Contract: DeleteObject — `DELETE /<bucket>/<key>`

| Condition | Status | Code / body |
|-----------|--------|-------------|
| Invalid bucket name / missing bucket | 400 / 404 | `InvalidBucketName` / `NoSuchBucket` (gate, 006) |
| Key over 1024 bytes | 400 | `KeyTooLongError` |
| Key existed, or did not, or had a corrupt record | 204 | no body; only `x-amz-request-id` |
| Storage failure (record removal or `meta/` sync) | 500 | `InternalError`, logged as `storage_error`; the object stays readable |

Conditional headers are ignored. `POST /<bucket>?delete` (DeleteObjects) stays 501.
