# Quickstart: Crash Recovery and Concurrency Guarantees

Contract: [contracts/recovery.md](contracts/recovery.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0, 100% for every `internal/` package, `coverage gate: PASSED`; `make e2e` includes the SIGKILL-during-20-uploads test and the 50-concurrent-PUTs test.

## Compose check

The runtime image has no shell, so a helper container (the pinned Go image) plants leftovers in the volume.

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
printf 'hello world' > /tmp/tiny-s3-hello.txt
s3 create-bucket --bucket demo >/dev/null
s3 put-object --bucket demo --key k --body /tmp/tiny-s3-hello.txt >/dev/null
docker compose logs s3d | grep '"msg":"recovery"'                                   # 1
docker compose kill -s SIGKILL s3d
GO_IMAGE=golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385
docker run --rm -v tiny-s3_data:/data "$GO_IMAGE" sh -c 'echo x > /data/tmp/put-leftover; echo y > /data/data/orphan'
docker compose up -d
sleep 1; docker compose logs s3d | grep '"msg":"recovery"' | tail -1               # 2
s3 get-object --bucket demo --key k /tmp/tiny-s3-out.txt --query ETag                # 3
docker run --rm -v tiny-s3_data:/data "$GO_IMAGE" sh -c 'ls /data/tmp | wc -l; ls /data/data | wc -l'   # 4
rm -f /tmp/tiny-s3-hello.txt /tmp/tiny-s3-out.txt
docker compose down -v
```

Expected:

1. `"tmp_removed":0,"orphans_removed":0,"objects_indexed":0,"records_skipped":0,"buckets":0` (first start, empty volume).
2. `"tmp_removed":1,"orphans_removed":1,"objects_indexed":1,"records_skipped":0,"buckets":1`.
3. `"\"5eb63bbbe01eeed093cb22bb8f5acdc3\""`.
4. `0` then `1`.
