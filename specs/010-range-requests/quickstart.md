# Quickstart: Range Requests

Contract: [contracts/http-range.md](contracts/http-range.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0, 100% for every `internal/` package, `coverage gate: PASSED`; `make e2e` includes the CLI range cases, `If-Range`, and a 1 GiB range of a 2 GiB object with peak RSS below 64 MiB.

## Compose and AWS CLI check

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
head -c 1000 /dev/urandom > /tmp/tiny-s3-in.bin
s3 create-bucket --bucket demo >/dev/null
s3 put-object --bucket demo --key r.bin --body /tmp/tiny-s3-in.bin >/dev/null
get() { s3 get-object --bucket demo --key r.bin --range "$1" /tmp/tiny-s3-out.bin --query '[ContentRange,ContentLength]' --output text; }
get bytes=0-99;   cmp <(head -c 100 /tmp/tiny-s3-in.bin) /tmp/tiny-s3-out.bin && echo same   # 1
get bytes=100-;   cmp <(tail -c 900 /tmp/tiny-s3-in.bin) /tmp/tiny-s3-out.bin && echo same   # 2
get bytes=-50;    cmp <(tail -c 50 /tmp/tiny-s3-in.bin) /tmp/tiny-s3-out.bin && echo same    # 3
get bytes=990-5000                                                                          # 4
get bytes=1000-                                                                             # 5
get bytes=0-1,5-6                                                                           # 6
get bytes=abc                                                                               # 7
etag=$(s3 head-object --bucket demo --key r.bin --query ETag --output text)
curl -s -o /dev/null -w '%{http_code}\n' -H 'Range: bytes=0-9' -H 'If-Range: "stale"' http://localhost:9000/demo/r.bin   # 8
curl -s -o /dev/null -w '%{http_code}\n' -H 'Range: bytes=0-9' -H "If-Range: $etag" http://localhost:9000/demo/r.bin     # 9
rm -f /tmp/tiny-s3-in.bin /tmp/tiny-s3-out.bin
docker compose down -v
```

Expected:

1. `bytes 0-99/1000	100` and `same`.
2. `bytes 100-999/1000	900` and `same`.
3. `bytes 950-999/1000	50` and `same`.
4. `bytes 990-999/1000	10` (end clamped).
5. Exit 254, `(InvalidRange)`.
6. `None	1000` (whole object, no Content-Range).
7. `None	1000`.
8. `200`.
9. `206`.
