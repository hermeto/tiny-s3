# Implementation Plan: Server Skeleton

**Branch**: `001-server-skeleton` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/001-server-skeleton/spec.md`

## Summary

Lay down the repository skeleton for tiny-s3: a Go module, a thin `cmd/s3d` binary, and five `internal/`
packages (`config`, `fsx`, `s3api`, `server`, and `app` to wire them) that parse startup flags, verify the
data directory through the fault-injection layer, answer every request with an S3 `NotImplemented` error, and
drain in-flight requests on SIGTERM/SIGINT within 10 seconds. A multi-stage Dockerfile produces a static binary
on distroless `nonroot`; `compose.yaml` runs it on port 9000 with a named volume. A Makefile runs
`test`, `race`, `vet` and `cover` inside a pinned golang container, and `scripts/cover-gate.sh` enforces 100%
statement coverage per `internal/` package. `make e2e` runs the built binary and sends it real signals. See [research.md](research.md) for every decision.

## Technical Context

**Language/Version**: Go 1.27.3 (`go 1.27` in `go.mod`, `GOTOOLCHAIN=local`)

**Primary Dependencies**: Go standard library only (`net/http`, `flag`, `log/slog`, `os/signal`, `encoding/xml`, `crypto/rand`)

**Storage**: Local filesystem directory (`/data` in the container) on a named Docker volume; no files written by this feature beyond a transient probe

**Testing**: `go test`, `go test -race`, `go vet`, coverage profile + `scripts/cover-gate.sh`; all inside `golang:1.27.3-trixie` (digest-pinned)

**Target Platform**: Linux containers (linux/arm64 and linux/amd64) on Docker Desktop for macOS

**Project Type**: Single Go service (HTTP server binary)

**Performance Goals**: Idle stop under 1 s; worst-case stop under 10 s from first signal

**Constraints**: No host Go; GNU make 3.81 compatibility; non-root runtime; no third-party modules; 100% statement coverage under `internal/`

**Scale/Scope**: 5 small `internal/` packages, 1 binary, 1 Dockerfile, 1 Compose file, 1 Makefile, 1 script

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Durability Before Acknowledgement | N/A (pass) | No object writes. The data-dir probe is not an acknowledged write; it is created and removed before listening. |
| II. Streaming, Bounded Memory | Pass | The placeholder handler reads no body. No `ReadTimeout`/`WriteTimeout` is set so later streaming handlers are not constrained by a global deadline (contracts/http.md). |
| III. Own Engine, Standard Library First | Pass | Standard library only; `go.mod` has no `require`. |
| IV. Test-First, 100% Coverage | Pass | Tests precede code in tasks; `make cover` gate per R3, available from the Foundational phase; filesystem failures go through new `internal/fsx` (R4); `cmd/s3d` stays thin and is covered by the standard-library e2e test in `e2e/` (`make e2e`, R11); all tests run under `-race`. |
| V. Real-Client Compatibility | Pass | Errors use the S3 XML format with `Code`, `Message`, `RequestId` and status 501 (R8). No S3 operation is implemented, so no `aws s3api` e2e test is required yet; the first operation feature adds the AWS CLI/SDK cases to `e2e/`. |
| Scope: everything in Docker, named volume | Pass | Compose named volume `data`; dev targets run in containers. |
| Scope: no CI, no metrics | Pass | Neither added. |
| Workflow: one feature = one PR on its own branch | **Action** | Branch `001-server-skeleton` does not exist yet; current branch is `000-bootstrap`. Create it before implementation. |
| Workflow: README design section | Pass | FR-015; trade-offs from research.md (distroless, 9 s drain, no health route, fsx now, flags only) go into the README. |

Post-design re-check: no violations. Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/001-server-skeleton/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── cli.md
│   ├── http.md
│   └── make.md
├── checklists/requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
go.mod                       # module github.com/hermeto/tiny-s3, go 1.27
cmd/s3d/main.go              # signal.NotifyContext + os.Exit(app.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, fsx.OS{}))
internal/
├── app/                     # Main: parse config → check data dir → listen → log → server.Run → exit code
├── config/                  # Config struct, Parse(args, stderr) with flag.FlagSet (ContinueOnError)
├── fsx/                     # FS interface (MkdirAll, CreateTemp, File.Close, Remove), OS implementation, Faulty wrapper
├── s3api/                   # NewHandler() (501 placeholder), WriteError(w, r, code, msg, status), request IDs
└── server/                  # Run(ctx, ln, handler, shutdownTimeout, logger) error; ErrForcedShutdown
e2e/shutdown_test.go         # //go:build e2e; builds and runs the real s3d binary, sends real signals (R11)
scripts/cover-gate.sh        # per-package 100% statement coverage gate
Dockerfile                   # golang:1.27.3-trixie@sha256:… build → distroless/static-debian13:nonroot@sha256:…
.dockerignore                # .git, specs, coverage files, .specify, .claude
compose.yaml                 # name: tiny-s3; service s3d; ports 9000:9000; volume data:/data; stop_grace_period: 15s
Makefile                     # test, race, vet, cover, e2e, clean-cache; DOCKER_RUN with pinned image and cache volumes
README.md                    # usage, targets, design section with this feature's trade-offs
```

**Structure Decision**: Single Go module. `cmd/s3d` holds only process concerns (signals, `os.Exit`), so every
decision with a branch lives in `internal/` and is covered by the gate. `internal/app` takes the `fsx.FS`, the
writers and the context as parameters, which lets its tests drive startup failures, a cancelled context and a
forced shutdown without a real process. Package dependency direction:
`cmd/s3d → app → {config, fsx, s3api, server}`; `server` and `s3api` do not import each other.

## Key implementation notes

- **Listener first, then `Run`**: `app` opens the listener with `net.Listen` so it can log the real address
  (needed for `-listen=:0` in tests) and report bind errors as startup failures before serving.
- **Shutdown**: on `ctx.Done()`, `server.Shutdown(context.WithTimeout(context.Background(), 9s))`; on
  `context.DeadlineExceeded`, `Close()` and return `ErrForcedShutdown`. The `Shutdown` context is derived from
  `Background`, not from the cancelled signal context.
- **Second signal**: `main` never calls the `stop` function from `signal.NotifyContext` before exit, so further
  signals are absorbed. Tests in `app` cancel the context (the same code path inside `internal/`); the e2e test
  sends real signals to the built binary to cover `cmd/s3d` itself.
- **Test timings**: tests construct `config.Config` with `ShutdownTimeout` in milliseconds and use channels, not
  sleeps, to order events, so `-race` runs stay deterministic.
- **Compose volume name**: top-level `name: tiny-s3` pins the project name so the volume is `tiny-s3_data`
  regardless of the checkout directory name.
- **Make on macOS**: `PWD`-based bind mount (`-v "$(CURDIR)":/src`), `--rm`, `-t` only when stdout is a TTY, so
  output and exit codes work in scripts.

## Complexity Tracking

No violations to justify.
