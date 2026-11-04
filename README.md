# tiny-s3

tiny-s3 is a minimal S3-compatible object store written in Go, with its own storage engine and only the
standard library in production code. This first version is the skeleton: a server that starts, checks its
data directory, answers every request with an S3 `NotImplemented` error, and shuts down cleanly. S3 operations
come in later features.

Everything runs in Docker. You need Docker (with Compose v2) and `make`; Go is not required on the host.

## Quick start

```sh
docker compose up -d --build              # server on localhost:9000, data in the tiny-s3_data volume
curl -si http://localhost:9000/any/path   # 501 with an S3 XML error body
docker compose stop                       # graceful stop, exit code 0
docker compose down -v                    # remove the container and the data volume
```

## Running s3d

```text
s3d [-listen ADDR] [-data-dir PATH]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-listen` | `:9000` | TCP address to listen on (`host:port`; empty host = all interfaces). |
| `-data-dir` | `/data` | Directory for object data. Created if missing (mode 0750); must be writable. |
| `-h` | | Print usage and exit 0. |

| Exit code | Meaning |
|-----------|---------|
| 0 | Stopped by SIGTERM/SIGINT after every in-flight request finished, or `-h`. |
| 1 | Data directory unusable, cannot listen, server error, or shutdown forced at the deadline. |
| 2 | Usage error (unknown flag, empty value, positional argument). |

On the first SIGTERM or SIGINT, s3d stops accepting connections, closes idle ones, and waits up to 9 seconds
for in-flight requests before closing the rest. It always exits within 10 seconds. Later signals are ignored
while it drains. Logs are JSON lines on stdout (`listening`, `shutdown_started`, `shutdown_complete`,
`shutdown_forced`); startup errors are one plain line on stderr (`s3d: <reason>`).

## Development

Every target runs in a digest-pinned `golang:1.27.3` container. Module and build caches live in the
`tiny-s3-gomod` and `tiny-s3-gobuild` volumes, so repeat runs download nothing.

| Target | What it runs |
|--------|--------------|
| `make test` | `go test ./...` |
| `make race` | `go test -race ./...` |
| `make vet` | `go vet -tags e2e ./...` |
| `make cover` | `scripts/cover-gate.sh`: fails unless every package under `internal/` has tests and 100% statement coverage |
| `make e2e` | Builds the real `s3d` binary and drives it with real signals (`e2e/`, build tag `e2e`, about 10 s) |
| `make clean-cache` | Removes the cache volumes |

A pull request is mergeable when `make test`, `make race` and `make cover` pass (see
`.specify/memory/constitution.md`). Run `make e2e` too.

Layout: `cmd/s3d` only handles signals and the exit code; `internal/app` wires everything; `internal/config`
parses flags; `internal/fsx` is the filesystem layer with fault injection; `internal/s3api` renders S3
responses; `internal/server` runs the HTTP server and its shutdown.

## Design

Trade-offs made so far, newest feature last.

### 001 server skeleton

- **Distroless `nonroot` runtime, digest-pinned images.** The binary is static, so the runtime image has no
  shell or package manager and runs as UID 65532. Both images are pinned by tag and digest for reproducible
  builds; updating them is a deliberate change. The image ships an empty `/data` owned by 65532, and Docker
  copies that ownership into an empty named volume, so no chown step or root entrypoint is needed.
- **9-second drain inside a 10-second budget.** The drain deadline leaves 1 second for closing connections and
  exiting. Docker's default stop grace period is also 10 seconds and would race that, so `compose.yaml` sets
  `stop_grace_period: 15s`. s3d is PID 1 (exec-form entrypoint, no init) and handles signals itself; it spawns
  no children, so nothing needs reaping.
- **Request contexts are not tied to the shutdown signal**, so in-flight requests finish instead of being
  cancelled when SIGTERM arrives.
- **No `ReadTimeout`/`WriteTimeout`.** Object bodies will be streamed, and a global deadline would cut off large
  transfers. Only `ReadHeaderTimeout` (10 s) is set, against slow-header clients; per-request deadlines come with
  the streaming operations.
- **No health-check route.** In path-style S3 any path can be a bucket name, so a reserved path would collide
  with S3 semantics. Readiness is a TCP connect to the port.
- **Placeholder 501 `NotImplemented` for every request**, in the S3 XML error format with a request ID in the
  body and in `x-amz-request-id`. Each S3 feature replaces it route by route.
- **Flags only, no environment variables.** One source of truth; `compose.yaml` sets both flags explicitly.
- **`internal/fsx` from the start.** The constitution requires I/O failures to be tested through a
  fault-injection layer. Permission-bit tests would not work anyway, because the dev container runs as root and
  root ignores them. The data directory check is `MkdirAll` plus a create/close/remove probe; `MkdirAll` already
  rejects a path that is a regular file, so there is no separate `Stat`.
- **Coverage gate counts statements, not percentages.** `scripts/cover-gate.sh` sums covered and total
  statements per package from the profile and fails on any difference, so 2499/2500 fails even though it prints
  as 100.0%. Packages without tests are found with `go list`, because since Go 1.22 they still appear in the
  profile with zero hits.
- **e2e tests hold requests open from the client side.** The placeholder handler answers at once, so the e2e
  test sends a complete header and half of a small declared body. Before flushing the response, `net/http`
  drains the unread body (under 256 KiB), which keeps the connection active until the client sends the rest:
  sending it tests the clean drain, withholding it tests the forced 9-second path. A request whose header is
  still incomplete is never treated as in flight: `net/http` closes such a connection as idle once it is 5
  seconds old, and drops a header completed after shutdown began.
