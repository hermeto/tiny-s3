# Contract: GetObject `GET /<bucket>/<key>` and HeadObject `HEAD /<bucket>/<key>`

Routing (003) and the bucket gate (006) are unchanged; every response carries `x-amz-request-id`.

## Success (200)

Headers: `ETag`, `Content-Length`, `Content-Type`, `Last-Modified`, `Accept-Ranges: bytes`, `x-amz-meta-*`. GET streams the body; HEAD has none.

## Conditional headers

| Step | Header | Fails when | Answer |
|------|--------|-----------|--------|
| 1 | `If-Match` | no listed ETag matches (`*` matches) | 412 `PreconditionFailed` |
| 1' | `If-Unmodified-Since` (only without `If-Match`) | modified after the date | 412 `PreconditionFailed` |
| 2 | `If-None-Match` | a listed ETag matches (`*` matches) | 304 |
| 2' | `If-Modified-Since` (only without `If-None-Match`) | not modified after the date | 304 |

ETag entries: comma-separated, quoted or not, `W/` ignored. Dates: RFC 1123/850/ANSI C; unparseable dates are ignored. Last-modified is compared in whole seconds. 304 carries `ETag` and `Last-Modified`, no body. Existence is checked first: a missing key is 404 whatever the conditions.

## Errors

| Condition | Status | Code |
|-----------|--------|------|
| Missing key | 404 | `NoSuchKey` (HEAD: no body) |
| Precondition | 412 | `PreconditionFailed` (HEAD: no body) |
| Storage failure before the status | 500 | `InternalError` |
| Storage or client failure after the status | — | connection aborted |
