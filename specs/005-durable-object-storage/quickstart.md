# Quickstart: Durable Object Storage

The storage engine is an internal package and is not wired to HTTP yet (research R9), so validation runs the unit, race and coverage gates, then a Compose smoke check proving the server still behaves as before.

## Prerequisites

- Docker with Compose v2
- `make`
- AWS CLI v2

## Quality gates

From the repository root:

```sh
make vet test race cover e2e
```

Expected: every target exits 0; `make cover` lists `github.com/hermeto/tiny-s3/internal/storage` and `.../internal/fsx` at 100.0% and ends with `coverage gate: PASSED`.

What the storage tests prove (see [spec.md](spec.md) and [contracts/storage-api.md](contracts/storage-api.md)):

- Put/Head/Get/Delete round trip, zero-length and unusual keys.
- IncompleteBody (short and long body), BadDigest, EntityTooLarge, KeyTooLongError, invalid arguments, each leaving no files behind.
- A failure injected at every operation of a successful Put's history leaves no visible new version and an empty `tmp/`.
- A 64 MiB Put allocates less than 4 MiB.
- An open reader survives overwrite and delete; 16 concurrent Puts to one key end with exactly one complete winner, under `-race`.

## Compose and AWS CLI smoke check

```sh
docker compose up -d --build
curl -s http://localhost:9000/example/hello.txt
body=$(mktemp)
printf 'hello' > "$body"
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 \
  aws --endpoint-url http://localhost:9000 s3api put-object \
  --bucket example --key hello.txt --body "$body"
rm -f "$body"
docker compose down -v
```

Expected:

- `curl` prints an S3 XML error with `<Code>NotImplemented</Code>`.
- `aws s3api put-object` fails with `An error occurred (NotImplemented) when calling the PutObject operation`, because PutObject is wired by a later feature. A different error (connection refused, InternalError) means the change broke the server.
- `docker compose down -v` removes the container and the `tiny-s3_data` volume.
