# Quickstart: aws-chunked Decoding

Contract: [contracts/aws-chunked.md](contracts/aws-chunked.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0, 100% for every `internal/` package (including the new `internal/awschunked`), `coverage gate: PASSED`. `make e2e` includes the aws CLI uploads through a local TLS proxy (default chunked framing with a CRC64NVME trailer, and `--checksum-algorithm` CRC32, CRC32C, SHA1, SHA256), signed framing, and the rejection cases.

## Compose check

The default CLI over plain HTTP sends a checksum header; the chunked framing is sent by hand with curl.

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
printf 'hello world' > /tmp/tiny-s3-hello.txt
s3 create-bucket --bucket demo                                                    # 1
s3 put-object --bucket demo --key header.txt --body /tmp/tiny-s3-hello.txt        # 2
s3 put-object --bucket demo --key sha.txt --body /tmp/tiny-s3-hello.txt --checksum-algorithm SHA256   # 3
s3 put-object --bucket demo --key bad.txt --body /tmp/tiny-s3-hello.txt --checksum-crc32 AAAAAA==      # 4
chunked() {  # $1 = trailer value
  printf 'b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:%s\r\n\r\n' "$1" |
  curl -s -X PUT --data-binary @- \
    -H 'Content-Encoding: aws-chunked' -H 'X-Amz-Content-SHA256: STREAMING-UNSIGNED-PAYLOAD-TRAILER' \
    -H 'X-Amz-Decoded-Content-Length: 11' -H 'X-Amz-Trailer: x-amz-checksum-crc64nvme' \
    -D - -o /dev/null http://localhost:9000/demo/chunked.txt | grep -i -E '^(HTTP|ETag)'
}
chunked 'jSnVw/bqjr4='                                                            # 5
chunked 'AAAAAAAAAAA='                                                            # 6
rm /tmp/tiny-s3-hello.txt
docker compose down -v
```

Expected:

1. Exit 0.
2. Exit 0, ETag `"5eb63bbbe01eeed093cb22bb8f5acdc3"` (the CLI sent `x-amz-checksum-crc64nvme`, verified).
3. Exit 0, same ETag.
4. Exit 254, `(BadDigest)`.
5. `HTTP/1.1 200 OK` and `ETag: "5eb63bbbe01eeed093cb22bb8f5acdc3"`: the framing was removed.
6. `HTTP/1.1 400 Bad Request` (BadDigest).
