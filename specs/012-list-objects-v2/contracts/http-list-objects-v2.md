# Contract: ListObjectsV2 — `GET /<bucket>?list-type=2`

## Query parameters

| Parameter | Rule |
|-----------|------|
| `prefix` | Only keys starting with it |
| `delimiter` | Group the rest of the key up to and including the first delimiter into `CommonPrefixes` |
| `max-keys` | Non-negative integer; default 1000; values above 1000 use 1000; invalid → 400 `InvalidArgument` |
| `start-after` | Only keys strictly after it (ignored when a token is sent) |
| `continuation-token` | From `NextContinuationToken`; undecodable → 400 `InvalidArgument` |
| `encoding-type` | `url`, or absent; anything else → 400 `InvalidArgument` |
| `fetch-owner` | Ignored |

## Response (200, `application/xml`)

```xml
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>demo</Name><Prefix>dir/</Prefix><Delimiter>/</Delimiter><MaxKeys>1000</MaxKeys>
  <EncodingType>url</EncodingType><KeyCount>2</KeyCount><IsTruncated>false</IsTruncated>
  <Contents><Key>dir/f1</Key><LastModified>2026-11-05T10:00:00.000Z</LastModified>
    <ETag>"5eb63bbbe01eeed093cb22bb8f5acdc3"</ETag><Size>11</Size><StorageClass>STANDARD</StorageClass></Contents>
  <CommonPrefixes><Prefix>dir/sub/</Prefix></CommonPrefixes>
</ListBucketResult>
```

Elements present only when meaningful: `Delimiter`, `EncodingType`, `ContinuationToken`, `NextContinuationToken` (when truncated), `StartAfter`. `KeyCount` = keys + common prefixes in this page. With `encoding-type=url`, `Prefix`, `Delimiter`, `StartAfter`, `Key` and `CommonPrefixes/Prefix` are URL-encoded (unreserved and `/` kept, everything else `%XX`).

## Errors

Missing bucket 404 `NoSuchBucket`, invalid name 400 `InvalidBucketName` (gate, 006); invalid parameters 400 `InvalidArgument`. ListObjects v1 (`GET /<bucket>` without `list-type=2`) stays 501.
