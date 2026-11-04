# Contract: bucket HTTP operations

Path-style only; routing is unchanged from `specs/003-s3-routing-errors/contracts/http.md`. Every response carries `x-amz-request-id`; errors use the S3 XML error body (no body for HEAD).

## Bucket gate

Runs after routing succeeds, for every request with a bucket except `PUT /<bucket>` (CreateBucket): HeadBucket, ListObjectsV2, PutObject, GetObject, HeadObject, DeleteObject.

| Bucket | Answer |
|--------|--------|
| Invalid name | `400 InvalidBucketName` |
| Missing | `404 NoSuchBucket` |
| Exists | The operation runs; an operation that is not implemented yet answers `501 NotImplemented` as before |

Requests rejected by routing (subresource, unsupported method, `InvalidURI`) never reach the gate.

## CreateBucket — `PUT /<bucket>`

| Condition | Status | Code / headers |
|-----------|--------|----------------|
| Invalid name | 400 | `InvalidBucketName` |
| Body over 64 KiB, unreadable, or not a `CreateBucketConfiguration` document | 400 | `MalformedXML` |
| `LocationConstraint` other than absent, empty or `us-east-1` | 400 | `InvalidLocationConstraint` |
| Bucket exists | 409 | `BucketAlreadyOwnedByYou` |
| Storage failure | 500 | `InternalError`, logged as `bucket_error` |
| Created | 200 | `Location: /<bucket>`, empty body |

Checks run in the order of the table. An empty or whitespace-only body means us-east-1.

## HeadBucket — `HEAD /<bucket>`

After the gate: `200`, `x-amz-bucket-region: us-east-1`, no body.

## ListBuckets — `GET /`

`200`, `Content-Type: application/xml`. Query parameters are ignored.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>tiny-s3</ID><DisplayName>tiny-s3</DisplayName></Owner>
  <Buckets>
    <Bucket><Name>alpha</Name><CreationDate>2026-11-04T14:30:15.123Z</CreationDate></Bucket>
  </Buckets>
</ListAllMyBucketsResult>
```

`<Buckets>` is present (empty) when there are no buckets. Buckets are sorted by name; `CreationDate` is UTC with milliseconds.

## Error codes added

| Code | Status | Message |
|------|--------|---------|
| `InvalidBucketName` | 400 | The specified bucket is not valid. |
| `NoSuchBucket` | 404 | The specified bucket does not exist. |
| `BucketAlreadyOwnedByYou` | 409 | Your previous request to create the named bucket succeeded and you already own it. |
| `MalformedXML` | 400 | The XML you provided was not well-formed or did not validate against our published schema. |
| `InvalidLocationConstraint` | 400 | The specified location constraint is not valid. |
