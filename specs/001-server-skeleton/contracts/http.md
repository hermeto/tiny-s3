# Contract: HTTP surface (placeholder)

Until S3 operations exist, every request, for any method and path, gets the same response.

```http
HTTP/1.1 501 Not Implemented
Content-Type: application/xml
x-amz-request-id: 4F2A9C1E7B3D8065

<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NotImplemented</Code><Message>A header or method you provided implies functionality that is not implemented.</Message><Resource>/some/path</Resource><RequestId>4F2A9C1E7B3D8065</RequestId></Error>
```

Guarantees:

- `RequestId` in the body equals the `x-amz-request-id` header and differs between requests.
- `Resource` is the request path, XML-escaped.
- The request body, if any, is not read beyond what `net/http` does by default.
- `HEAD` requests get the status and headers with no body, as `net/http` does for any handler.

Server timeouts: `ReadHeaderTimeout` 10 s (bounds slow-header clients); no `ReadTimeout` or `WriteTimeout`, because
later features stream large bodies and will set per-request deadlines instead.

Each S3 feature replaces this response for the routes it implements; the error writer in `internal/s3api` stays
the single place that renders S3 errors.
