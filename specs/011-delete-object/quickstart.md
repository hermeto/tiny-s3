# Quickstart: DeleteObject

Contract: [contracts/http-delete-object.md](contracts/http-delete-object.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0, 100% for every `internal/` package, `coverage gate: PASSED`; `make e2e` includes delete-object twice, missing bucket, a SIGKILL right after an acknowledged delete, and a delete during a download.

## Compose and AWS CLI check

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
printf 'hello world' > /tmp/tiny-s3-hello.txt
s3 create-bucket --bucket demo >/dev/null
s3 put-object --bucket demo --key k --body /tmp/tiny-s3-hello.txt >/dev/null
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE http://localhost:9000/demo/k    # 1
s3 delete-object --bucket demo --key k                                            # 2
s3 head-object --bucket demo --key k                                              # 3
s3 delete-object --bucket missing --key k                                         # 4
s3 put-object --bucket demo --key k2 --body /tmp/tiny-s3-hello.txt >/dev/null
s3 delete-object --bucket demo --key k2 && docker compose kill -s SIGKILL s3d && docker compose up -d
sleep 1; s3 head-object --bucket demo --key k2                                    # 5
rm /tmp/tiny-s3-hello.txt
docker compose down -v
```

Expected:

1. `204`.
2. Exit 0 (the key no longer exists; still 204).
3. Exit 254, `(404)`.
4. Exit 254, `(NoSuchBucket)`.
5. Exit 254, `(404)`: the acknowledged delete survived the kill.
