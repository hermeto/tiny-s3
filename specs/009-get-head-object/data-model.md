# Data Model: GetObject and HeadObject

No stored data changes. The response is built from `storage.Info` (005, 007):

| Info field | Response header |
|------------|-----------------|
| MD5 | `ETag: "<hex>"` |
| Size | `Content-Length` |
| ContentType | `Content-Type` (`binary/octet-stream` for pre-007 objects) |
| LastModified | `Last-Modified`, RFC 1123 GMT, whole seconds |
| Metadata[name] | `x-amz-meta-<name>` |
| (constant) | `Accept-Ranges: bytes` |
