# tiny-s3

tiny-s3 is a minimal S3-compatible object store written in Go, with its own storage engine and only the
standard library in production code. So far it is the server and the S3 protocol layer: it starts, checks its
data directory, routes path-style S3 requests to their operation, answers in the S3 XML error format, and shuts
down cleanly. The operations themselves come in later features; until then they answer `501 NotImplemented`.

Everything runs in Docker. You need Docker (with Compose v2) and `make`; Go is not required on the host.

## Quick start

```sh
docker compose up -d --build              # server on localhost:9000, data in the tiny-s3_data volume
curl -si http://localhost:9000/any/path   # 501 NotImplemented with an S3 XML error body
curl -si -X PATCH http://localhost:9000/b # 405 MethodNotAllowed
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
`shutdown_forced`, `handler_panic`); startup errors are one plain line on stderr (`s3d: <reason>`).

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

A pull request is mergeable when the CI checks below pass (see `.specify/memory/constitution.md`). Run the
same targets locally before pushing, and `make e2e` too.

To use other cache locations, set `GOMOD_CACHE` and `GOBUILD_CACHE`: a bare name is a Docker volume, an absolute
path is a bind mount (`make test GOBUILD_CACHE="$PWD/.tmp/gobuild"`). CI uses this to save the caches between
runs.

Layout: `cmd/s3d` only handles signals and the exit code; `internal/app` wires everything; `internal/config`
parses flags; `internal/fsx` is the filesystem layer with fault injection; `internal/s3api` routes S3
requests, assigns request IDs and renders S3 errors; `internal/server` runs the HTTP server and its shutdown.

## CI

GitHub Actions (`.github/workflows/ci.yml`) runs on every push to any branch and on every pull request to `main`.
Each check is named after the command that reproduces it locally:

| Check | Reproduce with |
|-------|----------------|
| `make vet` | `make vet` |
| `make test` | `make test` |
| `make race` | `make race` |
| `make cover` | `make cover` |
| `docker build` | `docker build .` |

The checks run in parallel and independently, so one failure does not hide another. A new push to a branch
cancels that branch's run still in progress. Go module and build caches, and the image layer cache, are saved
from `main` and restored by every run.

On a push to `main`, once all five checks pass, a `publish` job pushes the image to
`ghcr.io/hermeto/tiny-s3`, tagged with the full commit SHA and `latest` (linux/amd64):

```sh
docker pull ghcr.io/hermeto/tiny-s3:latest
```

CI runs on linux/amd64; on Apple Silicon, local runs use the arm64 variant of the same pinned image. A failure
that depends on the architecture (timing- or race-sensitive tests) may show up only in CI. The CI result is
authoritative: treat it as a bug, not a flake. Reproducing it under amd64 emulation is best-effort, and `-race`
may not work under emulation.

## Protecting main

The checks only block merges once `main` has a ruleset that requires them. This needs repository admin and is
done once.

1. Settings → Rules → Rulesets → New ruleset → New branch ruleset.
2. Name it `main`, set Enforcement status to Active, and add the target "Include default branch".
3. Enable Restrict deletions and Block force pushes.
4. Enable Require a pull request before merging (0 required approvals works for a single maintainer).
5. Enable Require status checks to pass, tick Require branches to be up to date before merging, and add
   `make vet`, `make test`, `make race`, `make cover` and `docker build` with GitHub Actions as the source.
   A check appears in the picker once it has run at least once in the last week.
6. Save.

The same ruleset from the command line (`15368` is the GitHub Actions app ID):

```sh
gh api --method POST repos/hermeto/tiny-s3/rulesets --input - <<'EOF'
{
  "name": "main",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "pull_request", "parameters": {
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": false,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": false } },
    { "type": "required_status_checks", "parameters": {
        "strict_required_status_checks_policy": true,
        "required_status_checks": [
          { "context": "make vet", "integration_id": 15368 },
          { "context": "make test", "integration_id": 15368 },
          { "context": "make race", "integration_id": 15368 },
          { "context": "make cover", "integration_id": 15368 },
          { "context": "docker build", "integration_id": 15368 } ] } }
  ]
}
EOF
```

Renaming a job in `ci.yml` renames its check: update the ruleset in the same pull request, or every pull request
waits forever for a check that no longer exists.

The GHCR package is private when first published. To let anyone pull it: on the repository page, open
Packages → `tiny-s3` → Package settings → Danger Zone → Change visibility → Public.

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

### 002 CI pipeline

- **Gates call `make`, not `actions/setup-go`.** CI runs the same Makefile targets in the same digest-pinned
  container as developers, so there is one toolchain definition and CI cannot drift from local. The cost is
  pulling the golang image in every gate job.
- **Caches through overridable bind mounts.** Runners start with empty Docker volumes, so `GOMOD_CACHE` and
  `GOBUILD_CACHE` let CI mount runner directories that `actions/cache` saves and restores. They default to the
  named volumes, so local runs are unchanged. The names avoid Go's `GOMODCACHE`/`GOCACHE` because make imports
  the environment, and a host Go setup must not leak into the container.
- **Caches are saved only from `main`.** Every run restores; only successful pushes to `main` save, for the Go
  caches and the image layer cache. A branch can never seed a cache that `main` reads, and the 10 GB cache quota
  is not churned by short-lived branches. A branch's second push is therefore no faster than its first until
  `main` has a cache.
- **Push and pull request triggers both run.** A pull request branch is validated twice per commit, once per
  event. Accepted for simplicity; runner minutes are free for public repositories.
- **`main` runs are serialized, never cancelled.** They finish in push order, so `latest` never moves back to an
  older commit. GitHub keeps only the newest queued run, so when several commits land during a run, the
  intermediate ones get no SHA tag. A manual re-run of an older `main` run is the one case ordering does not
  cover, so `publish` tags `latest` only when the run's commit is still the tip of `main`; the SHA tag is always
  pushed.
- **Publish rebuilds from the layer cache** instead of passing the `docker build` image as an artifact. With a
  warm cache the layers are the ones the check built, without an upload/download round trip. Revisit if spec
  014 needs to run e2e against the exact built image. Provenance metadata includes build timestamps, so a re-run
  can push a new digest with the same layers.
- **Actions pinned by commit SHA, kept current by Dependabot.** Tags can be moved by whoever controls the action
  repository; SHAs cannot. Dependabot opens weekly pull requests that bump the SHA and its version comment.
- **Least privilege.** The workflow token is read-only; only `publish` gets `packages: write`. Checkout runs with
  `persist-credentials: false`, so no make target or build step can read the token from `.git/config`.
  `pull_request_target` is not used, so fork pull requests never run with write access.
- **Anonymous Docker Hub pulls.** No Docker Hub secret is stored. If pull limits start failing runs, add a
  read-only Docker Hub token as a secret and a login step in the gate and image jobs.

### 003 S3 routing and errors

- **Own router, not `http.ServeMux`.** `ServeMux` cleans paths and redirects, which would turn keys such as
  `a//b`, `./a` or `dir/../x` into other keys, and its patterns cannot match query parameters or headers. The
  router is one `switch` over method, path shape, subresources and `x-amz-copy-source`; the full table is in
  `specs/003-s3-routing-errors/contracts/http.md`.
- **Keys are decoded once, by `net/http`.** Bucket and key come from `r.URL.Path` as is; nothing in the router
  decodes again. `+` stays `+`, and `a%2Fb` is the same key as `a/b`, as in S3. A request target with invalid
  percent-encoding never reaches the router: `net/http` answers a plain-text `400` without a request ID. Making
  it an S3 error would need our own HTTP parser, for a request no real client sends.
- **501 versus 405.** A real S3 operation the server does not have (a subresource such as `?acl`, ListObjects
  v1, DeleteBucket, any `POST`, CopyObject) gets `501 NotImplemented`; a method S3 does not support, or one with
  no meaning on `/`, gets `405 MethodNotAllowed`. A subresource wins over everything, so `GET /b?list-type=2&acl`
  is `501`. CopyObject is detected by its header so it can never run as a PutObject with an empty body.
- **Fixed subresource list, other parameters ignored.** Routing looks only for the S3 subresource keys
  (case-sensitive, on presence). Unknown parameters are ignored, as S3 does, so SDK additions such as `x-id`
  and presign parameters never block a request. `versionId` and `partNumber` count as subresources: versioning
  and part reads are out of scope.
- **Path-style only.** Virtual-hosted-style requests are routed on their path and so reach the wrong bucket;
  clients must use path-style addressing, which the aws CLI does for `localhost` and IP endpoints.
- **One request ID per request, set before routing.** Every response carries `x-amz-request-id`, including
  successes; `WriteError` reuses it, so the header and `RequestId` always match.
- **Panics become `500 InternalError`.** A panicking operation is logged as `handler_panic` with its request ID.
  If the response has already started, the connection is aborted instead, so the client sees a failed transfer
  rather than a truncated success. The writer wrapper that tracks this implements `Unwrap`, so
  `http.ResponseController` still works for streaming handlers; it does not forward `io.ReaderFrom`, which
  GetObject can add if sendfile matters.

### 004 filesystem failure injection

- **One explicit `internal/fsx` seam.** The real `fsx.OS` adapter delegates directly to the operating system,
  while tests wrap it with `fsx.Faulty`. This keeps production behavior unchanged and prevents call-site mocks
  from hiding a filesystem operation that the storage engine performs.
- **Glob paths and Nth matching calls.** Fault rules match an operation plus a `path.Match` pattern and count
  only matching calls. The rule fires on its configured Nth match; this is more precise than a global call index
  when temporary files and unrelated directories are involved.
- **History records attempted calls, including failures.** The fake records operation, paths, byte counts and
  final error in a copy-safe ordered snapshot. Recording failed attempts makes durability-order assertions
  useful when the failure itself is the behavior under test.
- **Short writes are surfaced, not retried by the fake.** A configured write can return fewer bytes and a nil
  error is normalized to `io.ErrShortWrite`. Retry policy belongs to the storage caller, so the fake does not
  silently turn an incomplete write into success.
- **The object storage write path is not in this repository yet.** This feature provides and tests the complete
  seam, including the write/sync/rename/directory-sync sequence, so the future storage path can consume it
  without another abstraction or mock layer.

### 005 durable object storage

- **The metadata record is the commit point.** Put streams the body into `tmp/`, syncs it, renames it to an
  immutable `data/<version>` file and syncs `data/`; only then does it write the record to `tmp/`, sync it,
  rename it over `meta/<hash>.json` and sync `meta/`. A crash before the record rename leaves the old state;
  the cost is two file syncs and two directory syncs per Put.
- **Immutable data versions instead of overwrite in place.** Every Put writes a new data file and the record
  points at it, so a reader that opened a version keeps reading it after an overwrite or delete (POSIX keeps
  unlinked open files alive). The previous version is removed only after the new record is durable.
- **Hashed record names.** `meta/` holds `hex(sha256(len(bucket) ":" bucket key))`. No key can name a path
  outside the store, and the length prefix keeps the mapping unambiguous without relying on bucket-name
  rules. The record stores the real bucket and key, which a future index rebuild will read back.
- **Striped key locks, single process.** 64 mutexes serialize commit, Delete and Get's record-plus-data lookup
  per key, so concurrent Puts end with exactly one winner and no orphaned loser data. Uploads stream outside
  the lock. The locks are in-process, which is enough because one s3d owns a data directory.
- **Rollback when the `meta/` sync fails after the rename.** The engine restores the previous record (or
  removes the new one) so a failed Put never leaves the new version visible. If the rollback also fails, the
  error is returned and the new data is kept, so the key points at a complete version, never a partial one.
- **Best-effort cleanup after the commit.** Removing the previous version's data after a commit, or the data
  file after a Delete, does not fail the operation; a failure leaves an invisible orphan. `Open` clears
  `tmp/`, but orphans in `data/` (also from a crash between the data rename and the record commit) wait for
  the index-rebuild feature, which has to read every record anyway.
- **Flat directories.** `data/` and `meta/` have no hash fan-out. ext4 and XFS index large directories, and
  the record format names its data file, so fan-out can come later without a migration.
- **Not wired to HTTP yet.** The engine has no S3 endpoint in this feature; the PutObject feature maps its
  errors (IncompleteBody, BadDigest, EntityTooLarge, KeyTooLongError, NoSuchKey) to S3 responses and adds the
  `aws s3api` e2e tests.
