# Quickstart: GetObject and HeadObject

Contract: [contracts/http-get-head-object.md](contracts/http-get-head-object.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0, 100% for every `internal/` package, `coverage gate: PASSED`; `make e2e` includes get-object/head-object with the aws CLI, the conditional cases, the 2 GiB download with peak RSS below 64 MiB, and a download that survives an overwrite.

## Compose and AWS CLI check

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
head -c 1048576 /dev/urandom > /tmp/tiny-s3-in.bin
s3 create-bucket --bucket demo >/dev/null
s3 put-object --bucket demo --key a/b.bin --body /tmp/tiny-s3-in.bin --content-type application/x-demo --metadata owner=alice
s3 get-object --bucket demo --key a/b.bin /tmp/tiny-s3-out.bin                       # 1
cmp /tmp/tiny-s3-in.bin /tmp/tiny-s3-out.bin && echo identical                         # 2
s3 head-object --bucket demo --key a/b.bin                                            # 3
s3 head-object --bucket demo --key missing                                            # 4
s3 get-object --bucket demo --key missing /tmp/tiny-s3-out.bin                        # 5
etag=$(s3 head-object --bucket demo --key a/b.bin --query ETag --output text)
s3 get-object --bucket demo --key a/b.bin --if-none-match "$etag" /tmp/tiny-s3-out.bin   # 6
s3 get-object --bucket demo --key a/b.bin --if-match '"0000"' /tmp/tiny-s3-out.bin        # 7
s3 get-object --bucket demo --key a/b.bin --if-modified-since 2099-01-01 /tmp/tiny-s3-out.bin   # 8
rm -f /tmp/tiny-s3-in.bin /tmp/tiny-s3-out.bin
docker compose down -v
```

Expected:

1. Exit 0; output with `ETag`, `ContentLength: 1048576`, `ContentType: application/x-demo`, `LastModified`, `AcceptRanges: bytes`, `Metadata: {"owner": "alice"}`.
2. `identical`.
3. Exit 0; the same fields, no body written.
4. Exit 254, `(404)`.
5. Exit 254, `(NoSuchKey)`.
6. Exit 254, `(304)`.
7. Exit 254, `(PreconditionFailed)`.
8. Exit 254, `(304)`.
