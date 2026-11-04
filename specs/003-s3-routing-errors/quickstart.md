# Quickstart: S3 Request Routing and Error Responses

Validates the feature end to end. Contract: [contracts/http.md](contracts/http.md).

## Automated

```sh
make vet test race cover   # routing, error, request-ID and key tables in internal/s3api
make e2e                   # built binary: 501, 405, HEAD without body, request-ID header
```

All must pass; `make cover` must report 100% for every `internal/` package.

## Manual: curl

```sh
docker compose up -d --build

curl -si http://localhost:9000/                       # 501 NotImplemented (ListBuckets not built yet)
curl -si -X PATCH http://localhost:9000/b/k           # 405 MethodNotAllowed
curl -si -X DELETE http://localhost:9000/             # 405 MethodNotAllowed
curl -si 'http://localhost:9000/b?acl'                # 501 NotImplemented
curl -si http://localhost:9000//k                     # 400 InvalidURI
curl -sI http://localhost:9000/b/k                    # 501, x-amz-request-id present, no body
curl -si 'http://localhost:9000/b/a%2Fb%20c%25'       # 501 with <Resource>/b/a/b c%</Resource>
```

Expected for every response: an `x-amz-request-id` header; for non-HEAD errors, `Content-Type: application/xml`
and `<RequestId>` equal to that header.

## Manual: aws CLI (SC-003)

```sh
export AWS_ACCESS_KEY_ID=x AWS_SECRET_ACCESS_KEY=x AWS_DEFAULT_REGION=us-east-1
E=--endpoint-url=http://localhost:9000

aws $E s3api list-buckets
aws $E s3api create-bucket --bucket b
aws $E s3api head-bucket --bucket b
aws $E s3api list-objects-v2 --bucket b
aws $E s3api put-object --bucket b --key 'dir/relatório 100%.txt' --body README.md
aws $E s3api get-object --bucket b --key k /dev/null
aws $E s3api head-object --bucket b --key k
aws $E s3api delete-object --bucket b --key k
```

Expected: each command exits non-zero and prints
`An error occurred (NotImplemented) when calling the <Operation> operation: A header or method you provided
implies functionality that is not implemented.` For `head-bucket` and `head-object` the CLI has no body to read
and prints `An error occurred (501) when calling the HeadBucket operation: Not Implemented`. No command prints a
parse error. Add `--debug` to see the decoded key in the request path.

```sh
docker compose down -v
```
