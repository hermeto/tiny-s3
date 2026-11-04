# Contract: HTTP routing and errors

Supersedes `specs/001-server-skeleton/contracts/http.md`. Path-style only.

## Routing table

Evaluated top to bottom; the first match wins. `{b}` is a non-empty bucket, `{k}` a non-empty key, both decoded
once from the path. "Subresource" means any query key from the list below.

| # | Method | Path | Condition | Result |
|---|--------|------|-----------|--------|
| 1 | not `GET HEAD PUT POST DELETE` | any | | `405 MethodNotAllowed` |
| 2 | any | not `/`, empty bucket (`//`, `//k`) | | `400 InvalidURI` |
| 3 | any | bucket or key not valid UTF-8 | | `400 InvalidURI` |
| 4 | `GET` | `/` | | ListBuckets |
| 5 | `PUT POST DELETE HEAD` | `/` | | `405 MethodNotAllowed` |
| 6 | any | `/{b}`, `/{b}/`, `/{b}/{k}` | subresource present | `501 NotImplemented` |
| 7 | `GET` | `/{b}` | `list-type=2` | ListObjectsV2 |
| 8 | `GET` | `/{b}` | no `list-type`, or another value | `501 NotImplemented` |
| 9 | `PUT` | `/{b}` | | CreateBucket |
| 10 | `HEAD` | `/{b}` | | HeadBucket |
| 11 | `DELETE POST` | `/{b}` | | `501 NotImplemented` |
| 12 | `PUT` | `/{b}/{k}` | `x-amz-copy-source` header present | `501 NotImplemented` |
| 13 | `PUT` | `/{b}/{k}` | | PutObject |
| 14 | `GET` | `/{b}/{k}` | | GetObject |
| 15 | `HEAD` | `/{b}/{k}` | | HeadObject |
| 16 | `DELETE` | `/{b}/{k}` | | DeleteObject |
| 17 | `POST` | `/{b}/{k}` | | `501 NotImplemented` |

Subresources (case-sensitive, presence with any value): `accelerate acl analytics attributes cors delete
encryption intelligent-tiering inventory legal-hold lifecycle location logging metadataTable metrics
notification object-lock ownershipControls partNumber policy policyStatus publicAccessBlock replication
requestPayment restore retention select session tagging torrent uploadId uploads versionId versioning versions
website`. Other query parameters are ignored by routing. Malformed query pairs are skipped.

Until later features fill them in, the eight operations answer `501 NotImplemented`.

## Headers on every response

- `x-amz-request-id`: 16 uppercase hex characters, unique per request, set before the operation runs.

## Error response

```http
HTTP/1.1 405 Method Not Allowed
Content-Type: application/xml
x-amz-request-id: 4F2A9C1E7B3D8065

<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>MethodNotAllowed</Code><Message>The specified method is not allowed against this resource.</Message><Resource>/b/k</Resource><RequestId>4F2A9C1E7B3D8065</RequestId></Error>
```

| Code | Status | Message |
|------|--------|---------|
| `NotImplemented` | 501 | `A header or method you provided implies functionality that is not implemented.` |
| `MethodNotAllowed` | 405 | `The specified method is not allowed against this resource.` |
| `InvalidURI` | 400 | `Couldn't parse the specified URI.` |
| `InternalError` | 500 | `We encountered an internal error. Please try again.` |

- `Resource` is the decoded request path, XML-escaped. `RequestId` equals `x-amz-request-id`.
- `HEAD`: same status and headers, empty body.
- A panic in an operation: `500 InternalError` if the response has not started; otherwise the connection is
  aborted. Logged as `handler_panic` (JSON, stdout) with `request_id`, `method`, `path`, `panic`.

## Not covered

- Request targets with invalid percent-encoding: rejected by `net/http` with a plain-text `400 Bad Request`,
  no request ID.
- Virtual-hosted-style addressing.
