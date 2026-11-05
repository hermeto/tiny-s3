# Data Model: PutObject

## Object metadata record (extends 005)

`meta/<hash>.json`, format version 1, two new optional fields:

```json
{"v":1,"bucket":"demo","key":"a/b.txt","size":11,"md5":"5eb63bbbe01eeed093cb22bb8f5acdc3",
 "last_modified":"2026-11-04T14:30:15.123456789Z","data":"<version>",
 "content_type":"text/plain","metadata":{"owner":"alice"}}
```

| Field | Rule |
|-------|------|
| `content_type` | Stored as sent; `binary/octet-stream` when the request has none. Absent in older records → read as `binary/octet-stream`. |
| `metadata` | Lowercase names without the `x-amz-meta-` prefix → values as received (repeats joined with `,`). Total name + value bytes ≤ 2048. Absent → no metadata. |

## Info (storage.Info)

Adds `ContentType string` and `Metadata map[string]string` to Bucket, Key, Size, MD5, LastModified.

## PutOptions (storage.PutOptions)

| Field | Meaning |
|-------|---------|
| `ContentMD5` | Raw 16-byte digest from `Content-MD5`, or nil |
| `ContentType` | Value to store (empty → `binary/octet-stream`) |
| `Metadata` | User metadata to store (nil → none) |
