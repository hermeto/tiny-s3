# Quickstart: Filesystem Failure Injection

This feature is internal, so the quickstart validates the repository gates and the storage filesystem tests rather than adding a new S3 endpoint.

## Prerequisites

- Docker with Compose v2
- `make`
- AWS CLI v2 when running the repository's S3 e2e checks

## Unit and quality gates

From the repository root:

```sh
make vet
make test
make race
make cover
make e2e
```

Expected results: all commands exit 0; `make cover` reports 100% statement coverage for every package under `internal/`; the filesystem tests include create, write, file sync, rename, remove, directory create/open/sync failures, Nth matching calls, short writes, ordering, and cleanup.

## Docker Compose smoke check

```sh
docker compose up -d --build
curl -si http://localhost:9000/any/path
docker compose down -v
```

Expected result: the service starts, the existing S3-compatible placeholder response is returned, and `down -v` removes the container and named data volume.

## AWS CLI commands when applicable

The current repository has no implemented object operation for this feature. When the storage operation is available in the branch, use the same endpoint with path-style addressing:

```sh
aws --endpoint-url http://localhost:9000 s3api put-object --bucket example --key hello.txt --body ./hello.txt
aws --endpoint-url http://localhost:9000 s3api head-object --bucket example --key hello.txt
aws --endpoint-url http://localhost:9000 s3api get-object --bucket example --key hello.txt ./downloaded.txt
aws --endpoint-url http://localhost:9000 s3api delete-object --bucket example --key hello.txt
```

These commands are validation guidance only; they must not be used to claim this feature implements new S3 operations.
