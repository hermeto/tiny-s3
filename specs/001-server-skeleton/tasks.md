---

description: "Task list for the tiny-s3 server skeleton"
---

# Tasks: Server Skeleton

**Input**: Design documents from `/specs/001-server-skeleton/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ (cli.md, http.md, make.md), quickstart.md

**Tests**: Required. Constitution Principle IV mandates test-first, 100% statement coverage of `internal/`, and e2e coverage of `cmd/s3d`. Within each story, write the `_test.go` task first, run it, and confirm it fails (or does not compile) before the implementation task.

**Organization**: Tasks are grouped by user story. Go unit tests live next to the code (`internal/<pkg>/<file>_test.go`); the e2e test lives in `e2e/` behind the `e2e` build tag.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 = start/stop with Compose, US2 = data dir and listen address, US3 = containerized dev targets and coverage gate
- All commands run from the repository root. No Go on the host: every `go` command goes through `make` targets or the `DOCKER_RUN` prefix defined in the Makefile.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Branch, module, and a way to run Go tests without Go on the host.

- [X] T001 Create and switch to branch `001-server-skeleton` from `000-bootstrap` (`git switch -c 001-server-skeleton`) in the repository root
- [X] T002 Create `go.mod` with `module github.com/hermeto/tiny-s3` and `go 1.27`, no `require` lines (research.md R10)
- [X] T003 [P] Create `.dockerignore` excluding `.git`, `.claude`, `.specify`, `specs`, `coverage.*`, `*.out`, `*.test`, `bin`, `dist`, `data`
- [X] T004 Create `Makefile` with: `GO_IMAGE := golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385`; `TTY := $(shell test -t 0 && echo -t)` (stdin: `$(shell)` captures stdout); `DOCKER_RUN := docker run --rm $(TTY) -v "$(CURDIR)":/src -w /src -v tiny-s3-gomod:/go/pkg/mod -v tiny-s3-gobuild:/root/.cache/go-build -e GOTOOLCHAIN=local -e GOFLAGS=-mod=readonly $(GO_IMAGE)`; targets `test` (`go test ./...`), `race` (`go test -race ./...`), `vet` (`go vet -tags e2e ./...`); `.PHONY` for all targets. Use only GNU make 3.81 features (no `.ONESHELL`, no `$(file)`, no grouped targets) per research.md R2

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Startup configuration (used by every story) and the coverage gate (needed so every later checkpoint can prove 100%).

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Write `internal/config/config_test.go` (table-driven, `t.Parallel()`): no args → `Listen ":9000"`, `DataDir "/data"`, `ShutdownTimeout 9*time.Second`; `-listen=127.0.0.1:9100 -data-dir=/tmp/x/../y` → `Listen "127.0.0.1:9100"`, `DataDir "/tmp/y"` (cleaned with `filepath.Clean`); unknown flag `-bogus`, missing value `-listen`, empty value `-listen=`, empty value `-data-dir=`, and positional argument `extra` each return an error that `errors.Is(err, config.ErrUsage)` and write usage to the supplied `io.Writer`; `-h` and `-help` return `flag.ErrHelp`. Run `make test` and confirm it fails to compile
- [X] T006 Implement `internal/config/config.go`: `type Config struct { Listen, DataDir string; ShutdownTimeout time.Duration }`, `var ErrUsage = errors.New("usage error")`, `const DefaultShutdownTimeout = 9 * time.Second`, `func Parse(args []string, stderr io.Writer) (Config, error)` using `flag.NewFlagSet("s3d", flag.ContinueOnError)` with output set to `stderr`. Rules from data-model.md: "Non-empty" for both `Listen` and `DataDir`; "unknown flags, malformed values, empty values and positional arguments return a usage error"; `-h` / `-help` returns `flag.ErrHelp` unwrapped. Wrap every other error with `ErrUsage`
- [X] T007 Write `scripts/cover-gate.sh` (Bash, `#!/usr/bin/env bash`, `set -euo pipefail`, research.md R3): `mapfile -t pkgs < <(go list ./internal/...)`; `mapfile -t notests < <(go list -f '{{if not (or .TestGoFiles .XTestGoFiles)}}{{.ImportPath}}{{end}}' ./internal/...)`; `go test -covermode=atomic -coverprofile=coverage.out ./internal/...`; aggregate covered and total statements per package from `coverage.out` with `awk` (skip the `mode:` line; package = import path of the file's directory; a block is covered when its count > 0); a package listed in `pkgs` but absent from the profile is reported `0/0`. A package FAILS when `covered != total` (compare the integers, never the formatted percentage, so 2499/2500 fails even though it prints `100.0%`) or when it is in `notests` (suffix `FAIL (no tests)`). Print the table and verdict in the exact format of contracts/make.md; exit 1 on any failure, else print `coverage gate: PASSED` and exit 0; remove `coverage.out` on exit via `trap`
- [X] T008 Add to `Makefile`: `cover` → `$(DOCKER_RUN) bash scripts/cover-gate.sh`; `clean-cache` → `docker volume rm -f tiny-s3-gomod tiny-s3-gobuild`; add both to `.PHONY` (contracts/make.md)
- [X] T009 Lint the script with a pinned ShellCheck image: `docker run --rm -v "$PWD":/mnt -w /mnt koalaman/shellcheck:v0.10.0 scripts/cover-gate.sh` (tag resolved to `sha256:2097951f02e735b613f4a34de20c40f937a6c8f18ecb170612c88c34517221fb` on 2026-11-04) exits 0 (fix every finding in `scripts/cover-gate.sh`)
- [X] T010 Run `make test`, `make race`, `make vet`, `make cover`; all exit 0 and `make cover` shows `internal/config` at N/N

**Checkpoint**: Module builds in the container; config is parsed and tested; the coverage gate is available for every later checkpoint.

---

## Phase 3: User Story 1 - Start and stop the server with one command (Priority: P1) 🎯 MVP

**Goal**: `docker compose up` serves S3 `NotImplemented` errors on port 9000 with `/data` on a named volume, and SIGTERM/SIGINT drains in-flight requests and exits within 10 s.

**Independent Test**: `make e2e`, plus quickstart.md §2 and §3: `docker compose up -d --build`, `curl -si http://localhost:9000/any/path` returns 501 with the contracts/http.md body, `time docker compose stop s3d` finishes in under 1 s with exit code 0.

### Tests for User Story 1 ⚠️

> Write these first and confirm they fail before implementing.

- [X] T011 [P] [US1] Write `internal/s3api/errors_test.go` against `httptest.NewRecorder`: `WriteError(w, r, http.StatusNotImplemented, "NotImplemented", msg)` sets status 501, `Content-Type: application/xml`, body starts with `xml.Header`, unmarshals to `Code`, `Message`, `Resource`, `RequestId`; `RequestId` equals the `x-amz-request-id` header and matches `^[0-9A-F]{16}$`; two calls produce different IDs; a request path containing `&<>"` round-trips through `Resource` unchanged after unmarshal (proves escaping)
- [X] T012 [P] [US1] Write `internal/s3api/handler_test.go` with `httptest.NewServer(s3api.NewHandler())`: GET, PUT (with a body), DELETE and HEAD on `/`, `/bucket`, `/bucket/key/with/slashes` all return 501; non-HEAD bodies have `Code` `NotImplemented` and `Message` exactly `A header or method you provided implies functionality that is not implemented.`; HEAD has an empty body (contracts/http.md)
- [X] T013 [P] [US1] Write `internal/server/server_test.go` using `net.Listen("tcp", "127.0.0.1:0")`, a `slog.New(slog.NewJSONHandler(buf, nil))` logger on a mutex-guarded buffer, and channels (no sleeps) to order events: (a) serves a request and returns nil after cancel when idle, logging `shutdown_started` with `cause` and `shutdown_complete` with `elapsed`; (b) drain: handler signals `started`, blocks on `release`; after `cancel`, poll `net.Dial` to the address every 10ms until it is refused, failing the test if it is still accepted after 2s (the listener closes asynchronously inside `Shutdown`, so a single dial right after cancel is racy); close every probe connection that does connect immediately, because `Shutdown` waits for `StateNew` connections younger than 5s and an open probe would stall the drain; then close `release`; the in-flight client receives a full 200 body and `Run` returns nil; (c) an idle keep-alive connection (completed request, `Transport` with keep-alives) does not delay `Run` returning; (d) forced: `shutdownTimeout` 50ms, handler blocks until test cleanup, `Run` returns an error with `errors.Is(err, server.ErrForcedShutdown)`, log contains `shutdown_forced`; (e) `Run` on an already-closed listener returns a non-nil error that is not `http.ErrServerClosed`. Use `context.WithCancelCause` so (a) asserts the logged cause text
- [X] T014 [P] [US1] Write `internal/app/app_test.go` for `Main(ctx, args, stdout, stderr) int` and the unexported `run(ctx, cfg, stdout, stderr, h http.Handler) int`: with `-listen=127.0.0.1:0 -data-dir=<t.TempDir()>`, wait for the `listening` JSON line on stdout (use an `io.Pipe` or a guarded buffer polled with a channel), parse the real address, GET it and get 501, cancel → exit 0; `-bogus` → 2 with usage on stderr; `-h` → 0; listen on an address already held by the test → 1 with stderr `s3d: listen <addr>: ...`; `run` with a blocking handler and `ShutdownTimeout` 50ms → 1 with stderr containing `forced`
- [X] T015 [US1] Add to `Makefile`: `e2e` → `$(DOCKER_RUN) go test -tags e2e -count=1 ./e2e/...`; add to `.PHONY` (contracts/make.md)
- [X] T016 [US1] Write `e2e/shutdown_test.go` (`//go:build e2e`, package `e2e`, standard library only; research.md R11). `TestMain` builds the binary with `go build -o <tmpdir>/s3d ../cmd/s3d`. A helper starts it with `os/exec` using `-listen=127.0.0.1:0 -data-dir=<t.TempDir()>`, reads stdout until the `listening` JSON line, and returns the real address. Cases: (a) idle SIGTERM, repeated 20 times: each process exits 0 in under 1s (SC-002); (b) idle SIGINT: exit 0 in under 1s; (c) drain with a second signal: open a TCP connection and send `PUT /b/k HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\n01234` (complete header, half the body), wait 100ms, send SIGTERM, wait for `shutdown_started` on stdout, send a second SIGTERM, send `56789`, read a complete 501 response, and assert the process exits 0 within 10s of the first SIGTERM; (d) stalled header: send `GET /b/k HTTP/1.1\r\nHost: x\r\n` without the final blank line; after SIGTERM the server closes the connection (EOF) about 5s after it opened and the process exits 0 with `shutdown_complete`; (e) forced: the partial-body request from (c) never completes; the process exits 1 after at least 9s and within 10s of SIGTERM, with `shutdown_forced` on stdout and `shutdown forced at deadline` on stderr; (f) `-bogus` exits 2 with usage on stderr. Research.md R11 explains why a partial body, not a partial header, holds a request in flight. Run `make e2e` and confirm it fails (no `cmd/s3d` yet)

### Implementation for User Story 1

- [X] T017 [P] [US1] Implement `internal/s3api/errors.go`: `type Error struct { XMLName xml.Name \`xml:"Error"\`; Code, Message, Resource, RequestID string \`xml:"RequestId"\` }` (field order Code, Message, Resource, RequestId); `func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string)` that generates the ID, sets `Content-Type: application/xml` and `x-amz-request-id`, writes the status, then `xml.Header` and `xml.NewEncoder(w).Encode(...)`, discarding the encode error with `_ =` and a comment (headers already sent; client gone; research.md R8); `func newRequestID() string` returning `strings.ToUpper(hex.EncodeToString(b))` for 8 bytes from `crypto/rand.Read`
- [X] T018 [US1] Implement `internal/s3api/handler.go`: `func NewHandler() http.Handler` returning a handler that calls `WriteError(w, r, http.StatusNotImplemented, "NotImplemented", "A header or method you provided implies functionality that is not implemented.")` for every request (depends on T017)
- [X] T019 [P] [US1] Implement `internal/server/server.go`: `var ErrForcedShutdown = errors.New("shutdown forced at deadline")`; `func Run(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration, log *slog.Logger) error`; `http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}` with no `ReadTimeout`/`WriteTimeout` and default `BaseContext` (contracts/http.md, research.md R5); `Serve` in a goroutine sending to a buffered error channel; `select` on that channel (return the error) or `ctx.Done()` → log `shutdown_started` with `cause` = `context.Cause(ctx)`, call `Shutdown` with `context.WithTimeout(context.Background(), shutdownTimeout)`; on any `Shutdown` error call `Close`, log `shutdown_forced` with `elapsed`, return `fmt.Errorf("%w: %v", ErrForcedShutdown, err)`; otherwise log `shutdown_complete` with `elapsed` and return nil
- [X] T020 [US1] Implement `internal/app/app.go`: `func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int` → `config.Parse`; `flag.ErrHelp` → 0; other parse error → 2; otherwise `run(ctx, cfg, stdout, stderr, s3api.NewHandler())`. `run` builds `slog.New(slog.NewJSONHandler(stdout, nil))`, calls `net.Listen("tcp", cfg.Listen)` (error → `fmt.Fprintf(stderr, "s3d: listen %s: %v\n", ...)`, return 1), logs `listening` with `listen` = `ln.Addr().String()` and `data_dir`, calls `server.Run`; error → `fmt.Fprintf(stderr, "s3d: %v\n", err)`, return 1; nil → 0. Exit codes per contracts/cli.md (depends on T006, T018, T019)
- [X] T021 [US1] Implement `cmd/s3d/main.go`: `ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)` and `defer stop()`, with a comment that `os.Exit` skips the deferred call so the registration stays and later signals are absorbed (research.md R5), then `os.Exit(app.Main(ctx, os.Args[1:], os.Stdout, os.Stderr))`. No other logic
- [X] T022 [US1] Run `make test`, `make race`, `make vet`, `make cover`, `make e2e`; all exit 0 and every `internal/` package is at N/N
- [X] T023 [P] [US1] Create `Dockerfile`: stage `build` `FROM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385`, `WORKDIR /src`, copy `go.mod` first then the source, `CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags="-s -w" -o /out/s3d ./cmd/s3d`, `mkdir /out/data`; final stage `FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329`, `COPY --from=build /out/s3d /s3d`, `COPY --from=build --chown=65532:65532 /out/data /data`, `USER nonroot:nonroot`, `EXPOSE 9000`, `ENTRYPOINT ["/s3d"]` (exec form), `CMD ["-listen=:9000", "-data-dir=/data"]` (research.md R1, R6)
- [X] T024 [US1] Create `compose.yaml`: top-level `name: tiny-s3`; service `s3d` with `build: .`, `ports: ["9000:9000"]`, `command: ["-listen=:9000", "-data-dir=/data"]`, `volumes: ["data:/data"]`, `stop_grace_period: 15s`; top-level `volumes: { data: {} }` (named volume, not a bind mount; research.md R6)
- [X] T025 [US1] Validate quickstart.md §2, §3 and §5: `docker compose up -d --build`; `docker compose logs s3d` shows the `listening` line; `curl -si http://localhost:9000/any/path` returns 501 with matching `x-amz-request-id` and `RequestId`; `docker volume ls` shows `tiny-s3_data`; `docker compose exec s3d /s3d -h` exits 0; `time docker compose stop s3d` < 1 s and exit code 0 in `docker compose ps -a`; marker file survives `down`/`up`; `docker compose down -v`

**Checkpoint**: MVP. The server runs under Compose and stops cleanly; the real binary's signal handling is covered by `make e2e`.

---

## Phase 4: User Story 2 - Point the server at a data directory and listen address (Priority: P2)

**Goal**: The server creates and verifies its data directory through `internal/fsx` before listening, and reports bad directories and addresses clearly.

**Independent Test**: quickstart.md §6 plus unit tests in `internal/fsx` and `internal/app` that drive every data-dir failure through the `Faulty` wrapper, at 100% in `make cover`.

### Tests for User Story 2 ⚠️

- [X] T026 [P] [US2] Write `internal/fsx/fsx_test.go`: `fsx.OS{}` against `t.TempDir()` for `MkdirAll`, `CreateTemp` (returned `File.Name()` is inside the dir), `File.Close`, `Remove`; `fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{...}}` returns the injected error for the matching operation (`mkdirall`, `createtemp`, `close`, `remove`), matches an optional path prefix and ignores calls under other paths, and passes every non-matching call through to the wrapped `FS`; `Close` faults apply to files returned by `Faulty.CreateTemp`
- [X] T027 [P] [US2] Extend `internal/app/app_test.go` for the data directory (data-model.md startup sequence), adding a `fs fsx.FS` argument to every existing `Main`/`run` call written in T014: missing directory is created with mode `0o750` and the server starts; `-data-dir` pointing at a regular file → 1, stderr `s3d: data dir <path>: mkdir <path>: not a directory`; `Faulty` `mkdirall` → 1, stderr `s3d: data dir <path>: <err>`; `Faulty` `createtemp` → 1, stderr `s3d: data dir <path> not writable: <err>`; `Faulty` `close` → 1 and the probe file is removed; `Faulty` `remove` → 1; after a successful start the data directory contains no files; on every failure stdout has no `listening` line (server never listened); `-data-dir=` → 2. Also add a case to `e2e/shutdown_test.go`: `-data-dir=<path of a regular file>` exits 1 with `not a directory` on stderr

### Implementation for User Story 2

- [X] T028 [US2] Implement `internal/fsx/fsx.go` and `internal/fsx/faulty.go`: `type File interface { Name() string; Close() error }`; `type FS interface { MkdirAll(path string, perm fs.FileMode) error; CreateTemp(dir, pattern string) (File, error); Remove(path string) error }`; `type OS struct{}` delegating to `os`; `type Fault struct { Op, PathPrefix string; Err error }`; `type Faulty struct { FS FS; Faults []Fault }` whose methods return the first matching fault's `Err` (empty `PathPrefix` matches every path) and whose `CreateTemp` wraps the returned file so `close` faults apply (depends on T026)
- [X] T029 [US2] Implement `internal/app/datadir.go`: `func checkDataDir(fsys fsx.FS, dir string) error` following data-model.md exactly: "`MkdirAll(DataDir, 0o750)`. Error → "data dir {path}: {err}"" (no separate `Stat`: `MkdirAll` already fails with ENOTDIR for a regular file); "`CreateTemp(DataDir, ".probe-*")`, `Close`, `Remove`. Any error → "data dir {path} not writable: {err}""; "The probe file is removed on every path where it was created" (depends on T028)
- [X] T030 [US2] Update `internal/app/app.go`: add `fs fsx.FS` to `Main` and `run`; call `checkDataDir` before `net.Listen`; on error `fmt.Fprintf(stderr, "s3d: %v\n", err)` and return 1 (depends on T029)
- [X] T031 [US2] Update `cmd/s3d/main.go` to pass `fsx.OS{}` to `app.Main` (depends on T030)
- [X] T032 [US2] Run `make test`, `make race`, `make vet`, `make cover`, `make e2e` (all exit 0, every `internal/` package at N/N); then validate quickstart.md §6 with `docker compose run --rm s3d ...`: `-listen=:9100 -data-dir=/data/sub` logs both values; `-bogus` exits 2; `-data-dir=/etc/passwd` exits 1 with `mkdir /etc/passwd: not a directory`

**Checkpoint**: US1 and US2 both work; every startup failure exits non-zero before listening.

---

## Phase 5: User Story 3 - Run tests, race tests, vet and the coverage gate without Go on the host (Priority: P3)

**Goal**: Prove the developer targets and the coverage gate (built in Phase 2) meet US3 on a Mac with only Docker.

**Independent Test**: quickstart.md §1, including the gate check (drop a test → `make cover` exits 1 and names the package).

- [X] T033 [US3] Validate the gate (SC-005): `make cover` exits 0 with every `internal/` package at N/N; temporarily comment out one case in `internal/config/config_test.go` → exit 1 naming `internal/config`; temporarily add `internal/zzgate/zz.go` with one function and no test → exit 1 with `FAIL (no tests)`; revert both and confirm exit 0 again
- [X] T034 [US3] Validate SC-007: run `make test` twice; the second run pulls no image and downloads no modules (no `Pulling`/`go: downloading` lines)
- [X] T035 [US3] Validate SC-004 without host Go: `env PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin make test race vet cover e2e` (removes `/usr/local/go/bin` from `PATH`; `command -v go` must print nothing first) exits 0

**Checkpoint**: All three stories are independently functional; the merge gate (`make test`, `make race`, `make cover`) and `make e2e` pass.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T036 [P] Write `README.md`: what tiny-s3 is (one paragraph); quick start (`docker compose up`, `curl`, `docker compose stop`, `docker compose down -v`); `s3d` flags, defaults and exit codes (from contracts/cli.md); the `make` targets including `e2e` and `clean-cache` (from contracts/make.md); a "Design" section recording this feature's trade-offs: distroless `nonroot` runtime and digest-pinned images, 9 s drain inside a 10 s budget with a 15 s Compose grace period, no health route (bucket-name collision), `internal/fsx` introduced now (root ignores permission bits in the dev container), no `Stat` step (`MkdirAll` already rejects files), flags only (no env vars), no `ReadTimeout`/`WriteTimeout` because of future streaming, coverage gate compares integer statement counts from the profile, e2e holds requests open with a partial header
- [X] T037 Run `gofmt -l .` in the pinned golang container (`docker run --rm -v "$PWD":/src -w /src golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385 gofmt -l .`) and fix every `.go` file it lists
- [X] T038 Run the full merge gate and quickstart.md end to end: `make test && make race && make vet && make cover && make e2e`, then §2–§6; record the commands and outputs for the PR
- [X] T039 Re-read spec.md FR-001 to FR-015 and contracts/*.md against the implementation; update any contract that drifted in `specs/001-server-skeleton/contracts/`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none. T004 is needed before any test can run.
- **Foundational (Phase 2)**: depends on Phase 1. Blocks all stories (`config` is used by `app`; the gate is used at every checkpoint).
- **US1 (Phase 3)**: depends on Phase 2.
- **US2 (Phase 4)**: depends on Phase 2 and on T020/T021 from US1, because it extends `internal/app/app.go`, `internal/app/app_test.go`, `cmd/s3d/main.go` and `e2e/shutdown_test.go`. `internal/fsx` (T026, T028) has no dependency on US1 and can start right after Phase 2.
- **US3 (Phase 5)**: validation only; T033 needs Phase 2, T034–T035 need US1 and US2 so every target has code to run.
- **Polish (Phase 6)**: after all stories.

### Within Each User Story

- `_test.go` task first, confirmed failing, then the implementation task for the same package.
- `s3api` and `server` before `app`; `app` before `cmd/s3d`; Go code before `Dockerfile`/`compose.yaml` validation.
- T015 (`make e2e` target) before T016 so the e2e test can be run and seen failing.

### Parallel Opportunities

- T003 alongside T002/T004.
- US1 tests T011–T014 are all different files; T016 can be written alongside them once T015 is in.
- US1 implementations T017 and T019 (different packages); T023 Dockerfile can be written while T020–T021 are in progress.
- US2 T026 and T028 (`internal/fsx`) can run in parallel with all of US1.
- T036 README alongside T037.

---

## Parallel Example: User Story 1

```bash
# Tests first, all in different packages:
Task: "Write internal/s3api/errors_test.go"
Task: "Write internal/s3api/handler_test.go"
Task: "Write internal/server/server_test.go"
Task: "Write internal/app/app_test.go"
Task: "Write e2e/shutdown_test.go"

# Then independent implementations:
Task: "Implement internal/s3api/errors.go"
Task: "Implement internal/server/server.go"
Task: "Create Dockerfile"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Phase 1 → Phase 2 → Phase 3.
2. Stop and validate with T022 (`make e2e`) and T025 (quickstart.md §2, §3, §5).

### Incremental Delivery

1. Setup + Foundational → config tested in the container; coverage gate in place.
2. US1 → server under Compose with clean shutdown, real signals covered by e2e (MVP).
3. US2 → data directory verified through `fsx`, clear startup errors.
4. US3 → developer targets and gate validated on a Mac without host Go.
5. Polish → README design section, full quickstart run.

The constitution requires one PR for the whole feature, so these increments are commits on `001-server-skeleton`, not separate PRs.

---

## Notes

- Commit after each phase with Conventional Commits (`build:` for Makefile/Dockerfile/compose, `feat:` for server code, `test:` only for test-only commits, `docs:` for README).
- Every `internal/` package must pass `make cover` at each checkpoint from Phase 2 on.
- Unit tests use `t.Parallel()` where they don't share ports or global state, and channels instead of `time.Sleep` to stay deterministic under `-race`. The e2e test's single 100ms wait (T016) is the documented exception: it drives an external process with no other sync point.
