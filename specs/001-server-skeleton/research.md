# Research: Server Skeleton

Each entry records a decision, why it was made, and what else was considered. Image digests
were resolved on 2026-11-04 from Docker Hub and gcr.io.

## R1. Toolchain and base images

- **Decision**: Go 1.27.3. Build and dev image `golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385`.
  Runtime image `gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329`.
  Both are multi-arch indexes, so the same pin works on Apple Silicon (linux/arm64) and Intel (linux/amd64).
- **Rationale**: The race detector needs cgo and a C toolchain; the Debian-based golang image ships gcc,
  and the race runtime is supported on glibc linux/amd64 and linux/arm64. Pinning tag plus digest makes builds
  reproducible (constitution Principle III) while the tag keeps the pin readable. The server binary is static
  (`CGO_ENABLED=0`), so distroless static is enough: no shell, no package manager, a built-in `nonroot` user
  (UID/GID 65532).
- **Alternatives considered**: `golang:*-alpine` (musl; race support on musl is less exercised and needs
  `build-base` installed); `scratch` runtime (no `/etc/passwd`, no CA bundle, no non-root user without extra
  steps); `debian:trixie-slim` runtime (shell and package manager widen the attack surface for no gain).

## R2. Running the developer targets without Go on the host

- **Decision**: Each `make` target runs `docker run --rm` on the pinned golang image with the repository
  bind-mounted at `/src` (working directory), and two named volumes for caches: `tiny-s3-gomod` at
  `/go/pkg/mod` and `tiny-s3-gobuild` at `/root/.cache/go-build`. `GOTOOLCHAIN=local` and `GOFLAGS=-mod=readonly`
  are set so the container never downloads a different toolchain or edits `go.mod`.
- **Rationale**: Plain `docker run` in a Makefile is explicit and needs nothing beyond Docker and the
  `make` that ships with macOS. Named cache volumes make repeat runs fast (SC-007) and avoid writing root-owned
  cache files into the checkout.
- **Alternatives considered**: a `dev` Compose service with profiles (adds indirection, and `docker compose run`
  output and exit codes are noisier); a dev container image built from a Dockerfile (an extra build step for
  no extra tooling, since the stock image already has everything).
- **Constraint**: macOS ships GNU make 3.81. The Makefile uses only 3.81 features: no `.ONESHELL`, no
  `$(file ...)`, no grouped targets.

## R3. Coverage gate

- **Decision**: `scripts/cover-gate.sh` (Bash, `set -euo pipefail`, shellcheck-clean) runs inside the dev
  container. It lists packages with `go list ./internal/...`, runs
  `go test -covermode=atomic -coverprofile=coverage.out ./internal/...`, aggregates statement counts per
  package from the profile with `awk`, prints a table, and exits 1 if any package has `covered != total`
  (integer comparison; the percentage is display only) or has no test files. Packages without tests are found
  with `go list -f '{{if not (or .TestGoFiles .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./internal/...`, because
  since Go 1.22 they still appear in the profile with zero covered statements. `cmd/` is never listed.
- **Rationale**: Computing coverage from the profile counts statements exactly, instead of parsing the rounded
  percentage that `go test -cover` prints. Driving the list from `go list` means new packages are gated with no
  Makefile change (FR-013), and a package with no tests cannot slip through (spec US3 scenario 6).
- **Alternatives considered**: parsing `go test -cover` text output (format is not a stable interface, and
  rounding can show 100.0% for 99.96%); a Go program for the gate (would itself sit outside `internal/` and add
  code to maintain).

## R4. Fault injection for filesystem errors

- **Decision**: Introduce `internal/fsx` now with a minimal `FS` interface covering the calls this feature makes
  (`MkdirAll`, `CreateTemp`, `File.Close`, `Remove`), an `OS` implementation that delegates to `os`, and a `Faulty`
  wrapper that returns a configured error for a named operation. Later features add `Rename`, `Sync` and
  directory sync to the same interface.
- **Rationale**: Principle IV requires I/O failures to be tested through `internal/fsx`, not by mocking at call
  sites. Real-filesystem tricks (`chmod 0500`) do not work here anyway: the dev container runs as root, and root
  ignores permission bits, so a "read-only directory" test would pass for the wrong reason. Some branches (the
  probe file is created but cannot be removed) cannot be produced on a real filesystem at all.
- **Alternatives considered**: running tests as a non-root UID (fixes chmod tests but still leaves unreachable
  branches); skipping the writability probe (violates FR-002: the server would start and fail on the first write).

## R5. Graceful shutdown

- **Decision**: `cmd/s3d` creates the root context with `signal.NotifyContext(ctx, SIGTERM, SIGINT)` and keeps the
  registration until the process exits. `internal/server.Run` serves an `http.Server` on a pre-opened listener;
  when the context is cancelled it calls `Shutdown` with a 9-second deadline. If the deadline passes it calls
  `Close` and returns `ErrForcedShutdown`. Request contexts are not derived from the signal context
  (`BaseContext` is left default), so in-flight handlers are not cancelled by the signal.
- **Rationale**: `Shutdown` stops accepting connections, closes idle keep-alive connections immediately, and
  waits only for active ones, which is exactly the spec's edge case for idle connections. A 9-second drain
  leaves a 1-second margin for `Close`, log flush and exit inside the 10-second budget (FR-007). Keeping the
  signal registration means a second SIGTERM/SIGINT is absorbed instead of triggering the default "terminate"
  action, so the drain is not cut short.
- **Alternatives considered**: a full 10-second drain (exit could land just past 10 s); cancelling request
  contexts on signal (would abort the in-flight requests the spec says must finish).

## R6. PID 1, signals and the container user

- **Decision**: `ENTRYPOINT ["/s3d"]` in exec form, no shell and no init process. The Compose file sets
  `stop_grace_period: 15s`. The runtime stage copies an empty `/data` directory owned by `65532:65532` and runs as
  `USER nonroot:nonroot`.
- **Rationale**: Exec form makes `s3d` PID 1, so it receives SIGTERM directly. The kernel drops signals to PID 1
  only when no handler is installed; `signal.Notify` installs one. `s3d` spawns no children, so no zombie reaping is
  needed. Docker initializes an empty named volume from the image's content at the mount point, including
  ownership, so the non-root user can write `/data` without a chown step at runtime. A 15-second grace
  period guarantees the server's own 10-second deadline fires before Docker sends SIGKILL.
- **Alternatives considered**: `init: true` / tini (no children to reap, extra moving part); an entrypoint shell
  script that chowns the volume (needs a shell and root, both of which distroless deliberately lacks).

## R7. Startup configuration and exit codes

- **Decision**: Flags parsed with a `flag.FlagSet` in `ContinueOnError` mode: `-listen` (default `:9000`) and
  `-data-dir` (default `/data`). Exit codes: 0 clean exit or `-h`, 1 runtime failure (data dir, listen, serve
  error, forced shutdown), 2 usage error. Positional arguments are a usage error.
- **Rationale**: Flags are the boring default for a single binary, and the Compose file sets both explicitly so
  the effective values are visible. Exit code 2 for usage errors follows the `flag` package and common Unix
  practice.
- **Alternatives considered**: environment variables (a second source of truth to document and precedence rules to
  test; can be added later if a deployment needs them); a config file (nothing to configure yet that justifies one).

## R8. Placeholder response and request IDs

- **Decision**: `internal/s3api` serves every request with HTTP 501 and an S3 XML error body
  (`<Error><Code>NotImplemented</Code><Message>…</Message><Resource>{path}</Resource><RequestId>…</RequestId></Error>`),
  `Content-Type: application/xml`, and the same ID in the `x-amz-request-id` header. Request IDs are 16 uppercase
  hex characters from `crypto/rand`. The error writer is the shared helper later features use for every S3 error.
- **Rationale**: Principle V fixes the error format. `aws s3api` reads `x-amz-request-id` and prints the code and
  message, so the placeholder is already readable from the real client. Since Go 1.24 `crypto/rand.Read` never
  returns an error, so there is no untestable error branch. The body is written after headers are sent; an encode
  error at that point means the client went away and nothing useful can be done, so it is discarded explicitly.
- **Alternatives considered**: 404 or 405 for everything (misleading; S3 returns `NotImplemented` 501 for
  unsupported functionality); a dedicated health route (rejected in the spec: any path can be a bucket name).

## R9. Logging

- **Decision**: `log/slog` JSON handler on stdout. Startup logs `listen` and `data_dir`; shutdown logs
  `shutdown_started` and then `shutdown_complete` or `shutdown_forced` with the elapsed time. Startup errors go
  to stderr as plain text, so a failing `docker compose up` reads well.
- **Rationale**: Standard library, structured, and parseable by any log pipeline.
- **Alternatives considered**: text handler (harder to parse downstream); third-party loggers (Principle III).

## R10. Module path

- **Decision**: `github.com/hermeto/tiny-s3`, matching the `origin` remote. `go.mod` declares `go 1.27` and no
  `require` lines; the AWS SDK enters only with the first e2e test.
- **Rationale**: Matches where the code lives; Principle III keeps the production dependency set empty.

## R11. End-to-end test of the real binary

- **Decision**: `e2e/shutdown_test.go` (build tag `e2e`, standard library only) builds `cmd/s3d`, starts it with
  `os/exec`, and sends real SIGTERM/SIGINT. `make e2e` runs it in the dev container. An in-flight request is held
  open from the client side by sending a complete header plus half of a small declared body
  (`PUT /b/k HTTP/1.1`, `Content-Length: 10`, 5 body bytes). The placeholder handler answers at once, but before
  flushing the response `net/http` drains an unread body smaller than 256 KiB, which blocks on the client while the
  connection is `StateActive`; `Shutdown` waits for it. Sending the other 5 bytes completes the request (drain
  case, exit 0); withholding them hits the 9-second deadline (forced case, exit 1).
- **Finding during implementation**: a partial request *header* cannot hold a request in flight. The connection
  stays `StateNew` until the header is read (`net/http/server.go`, `setState(StateActive)` after `readRequest`);
  `Shutdown` closes `StateNew` connections older than 5 s as idle (`closeIdleConns`), and a header completed after
  `Shutdown` began is dropped without a response (`if c.server.shuttingDown() { return }`). The e2e test keeps
  this as its own case: a stalled header does not hold shutdown open, and the process exits 0.
- **Rationale**: Principle IV requires `cmd/s3d` to be covered by e2e tests, and the signal wiring there is the
  core of US1 (PID 1 signal delivery, exit codes, second-signal absorption, the 10-second bound). The partial-body
  technique reaches both the drain and the forced path against the placeholder handler, with no test code in the
  binary. It relies on documented-in-source `net/http` behaviour, so a Go upgrade that changes it fails the e2e
  test loudly rather than silently. The build tag keeps `make test` fast; the e2e run takes about 10 seconds.
- **Alternatives considered**: a debug-only slow route in the binary (test code in production); a partial
  request header (cannot be in flight, see above); waiting for the AWS SDK e2e harness in the first S3 feature
  (leaves this feature's main behaviour untested by automation).
