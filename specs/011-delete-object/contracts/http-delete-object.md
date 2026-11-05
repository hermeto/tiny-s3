# Contract: DeleteObject — `DELETE /<bucket>/<key>`

| Condition | Status | Code / body |
|-----------|--------|-------------|
| Invalid bucket name / missing bucket | 400 / 404 | `InvalidBucketName` / `NoSuchBucket` (gate, 006) |
| Key over 1024 bytes | 400 | `KeyTooLongError` |
| Key existed, or did not, or had a corrupt record | 204 | no body; only `x-amz-request-id` |
| Record removal fails | 500 | `InternalError`, logged as `storage_error`; the object stays readable |
| `meta/` sync after the removal fails | 500 | `InternalError`, logged; the body is kept, so if the record reappears after a crash it is complete |

Conditional headers are ignored. `POST /<bucket>?delete` (DeleteObjects) stays 501.
