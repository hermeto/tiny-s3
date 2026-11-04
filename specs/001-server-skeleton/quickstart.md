# Quickstart: validating the server skeleton

Prerequisites: macOS (Apple Silicon or Intel) with Docker Desktop running and no Go toolchain required. Run from
the repository root.

## 1. Developer targets (US3)

```sh
make test && make race && make vet && make cover && make e2e
```

Expected: all five exit 0; `make cover` prints every `internal/` package at 100.0% and `coverage gate: PASSED`.
Run `make test` a second time: no image pull or module download happens (SC-007).

Gate check (SC-005): comment out one test case in any `internal/` package, run `make cover`, and expect exit 1
with that package marked `FAIL`. Restore the test.

## 2. Start the server (US1)

```sh
docker compose up -d --build
docker compose logs s3d          # expect {"msg":"listening","listen":":9000","data_dir":"/data",...}
curl -si http://localhost:9000/any/path
```

Expected: `HTTP/1.1 501`, `Content-Type: application/xml`, an `x-amz-request-id` header, and the body from
[contracts/http.md](contracts/http.md) with a matching `RequestId`.

```sh
docker volume ls | grep tiny-s3          # named volume exists
docker compose exec s3d /s3d -h          # binary present; exits 0 (distroless has no shell)
```

## 3. Clean stop (US1, SC-002)

```sh
time docker compose stop s3d
docker compose ps -a --format '{{.Name}} {{.State}} {{.ExitCode}}'
```

Expected: completes in under 1 s; exit code 0; logs end with `shutdown_complete`.

## 4. Drain an in-flight request (SC-003)

The placeholder handler answers immediately, so `curl` cannot hold a request open. `make e2e` runs the real
binary and holds one open by sending a header plus half of a small declared body (research.md R11):

```sh
make e2e
```

Expected: 20 idle SIGTERM stops each exit 0 in under 1 s; SIGINT behaves the same; a request in progress at
SIGTERM, with a second SIGTERM sent mid-drain, completes with a 501 response once the rest of its body arrives
and the process exits 0; a request whose body never completes makes the process exit 1 between 9 and 10 s with
`shutdown_forced`; a connection that never finishes its header is closed about 5 s after it opened and the
process exits 0; a regular file as `-data-dir` exits 1 and `-bogus` exits 2.

## 5. Persistence across restarts (SC-006)

The skeleton writes no files, so check the volume directly:

```sh
docker run --rm -v tiny-s3_data:/data busybox:1.37 touch /data/marker
docker compose down && docker compose up -d
docker run --rm -v tiny-s3_data:/data busybox:1.37 ls /data   # marker is still there
docker compose down -v                                           # removes the volume
```

## 6. Configuration and startup failures (US2)

These are covered by unit tests in `internal/config` and `internal/app`. Spot-check with the image:

```sh
docker compose run --rm s3d -listen=:9100 -data-dir=/data/sub   # logs listen :9100, data_dir /data/sub
docker compose run --rm s3d -bogus; echo "exit=$?"              # usage on stderr, exit=2
docker compose run --rm s3d -data-dir=/etc/passwd; echo "exit=$?" # "s3d: data dir /etc/passwd: mkdir /etc/passwd: not a directory", exit=1
```
