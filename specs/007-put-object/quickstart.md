# Quickstart: PutObject

Contract: [contracts/http-put-object.md](contracts/http-put-object.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0; every `internal/` package at 100.0% and `coverage gate: PASSED`; `make e2e` includes the aws CLI put-object tests and the 2 GiB flat-memory test (peak RSS below 64 MiB).

## Compose and AWS CLI check

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
printf 'hello world' > /tmp/tiny-s3-hello.txt

s3 create-bucket --bucket demo                                                   # 1
s3 put-object --bucket demo --key a/b.txt --body /tmp/tiny-s3-hello.txt \
  --content-type text/plain --metadata owner=alice                                # 2
s3 put-object --bucket demo --key a/b.txt --body /tmp/tiny-s3-hello.txt \
  --content-md5 AAAAAAAAAAAAAAAAAAAAAA==                                          # 3
s3 put-object --bucket missing --key k --body /tmp/tiny-s3-hello.txt              # 4
s3 put-object --bucket demo --key "$(printf 'k%.0s' $(seq 1025))" --body /tmp/tiny-s3-hello.txt   # 5
curl -s -X PUT -H 'Transfer-Encoding: chunked' --data-binary @/tmp/tiny-s3-hello.txt \
  http://localhost:9000/demo/chunked                                             # 6
rm /tmp/tiny-s3-hello.txt
docker compose down -v
```

Expected:

1. Exit 0.
2. Exit 0, `"ETag": "\"5eb63bbbe01eeed093cb22bb8f5acdc3\""` (the MD5 of `hello world`).
3. Exit 254, `(BadDigest)`.
4. Exit 254, `(NoSuchBucket)`.
5. Exit 254, `(KeyTooLongError)`.
6. An S3 XML error with `<Code>MissingContentLength</Code>`.

Stored content type and metadata are checked by the `internal/app` integration test; reading them back over HTTP comes with feature 009.
