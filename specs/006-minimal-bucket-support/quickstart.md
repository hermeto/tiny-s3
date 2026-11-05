# Quickstart: Minimal Bucket Support

Validation runs the quality gates, then drives the server under Compose with the real AWS CLI v2. Contracts: [http-buckets.md](contracts/http-buckets.md), [storage-buckets.md](contracts/storage-buckets.md).

## Prerequisites

- Docker with Compose v2
- `make`
- AWS CLI v2 on the host (for the Compose check only; `make e2e` brings its own)

## Quality gates

From the repository root:

```sh
make vet test race cover e2e
```

Expected: every target exits 0; `make cover` lists every `internal/` package at 100.0% and ends with `coverage gate: PASSED`; `make e2e` builds the `tiny-s3-e2e` image and runs the `aws s3api` tests (create, re-create, head, list, restart, NoSuchBucket on all five object operations, InvalidBucketName, InvalidLocationConstraint).

## Compose and AWS CLI check

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }   # a function works in bash and zsh

s3 create-bucket --bucket demo                       # 1
s3 create-bucket --bucket demo                       # 2
s3 head-bucket --bucket demo                         # 3
s3 head-bucket --bucket missing                      # 4
s3 list-buckets                                      # 5
docker compose restart s3d
s3 list-buckets                                      # 6
s3 get-object --bucket missing --key k /dev/null     # 7
s3 create-bucket --bucket Bad_Name                   # 8
s3 create-bucket --bucket eu-bucket \
  --create-bucket-configuration LocationConstraint=eu-west-1   # 9
s3 get-object --bucket demo --key k /dev/null        # 10
docker compose down -v
```

Expected:

1. Exit 0, prints `{"Location": "/demo"}`.
2. Exit 254, `An error occurred (BucketAlreadyOwnedByYou) when calling the CreateBucket operation`.
3. Exit 0; the output includes `"BucketRegion": "us-east-1"`.
4. Exit 254, `An error occurred (404) when calling the HeadBucket operation: Not Found`.
5. Exit 0, `Buckets` holds `demo` with a `CreationDate`.
6. Same output as 5, with the same `CreationDate`: the bucket survived the restart.
7. Exit 254, `An error occurred (NoSuchBucket) when calling the GetObject operation`.
8. Exit 254, `An error occurred (InvalidBucketName) when calling the CreateBucket operation`.
9. Exit 254, `An error occurred (InvalidLocationConstraint) when calling the CreateBucket operation`.
10. Exit 254, `An error occurred (NotImplemented) when calling the GetObject operation`: the bucket exists, and GetObject is wired by a later feature.

`docker compose down -v` removes the container and the `tiny-s3_data` volume.
