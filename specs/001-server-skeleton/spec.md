# Feature Specification: Server Skeleton

**Feature Branch**: `001-server-skeleton`

**Created**: 2026-11-04

**Status**: Draft

**Input**: User description: "Skeleton for tiny-s3, a minimal S3-compatible object store. An operator can start the server with one command, point it at a data directory and a listen address, and stop it cleanly. A developer can run unit tests, race tests and a coverage gate with one command each, without installing Go on the host. Acceptance criteria: `docker compose up` starts the server on port 9000 with object data in a named volume. SIGTERM stops it within 10 seconds after in-flight requests finish. `make test`, `make race`, `make vet` and `make cover` work on a Mac with only Docker installed. `make cover` fails when any package under internal/ is below 100% statement coverage. Out of scope: any S3 operation."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Start and stop the server with one command (Priority: P1)

An operator clones the repository on a machine that has only Docker installed and runs `docker compose up`. The server starts, listens on port 9000 of the host, and keeps its data in a named volume. When the operator stops it (`docker compose down`, `docker compose stop` or Ctrl-C), the server stops accepting new connections, lets requests already in progress finish, and exits on its own before Docker has to kill it.

**Why this priority**: Every later feature (PutObject, GetObject, ListObjectsV2) is exercised through a running server. Without a server that starts, stores data in the right place and shuts down cleanly, nothing else can be built or tested end to end.

**Independent Test**: On a clean checkout, run `docker compose up -d`, send any HTTP request to `localhost:9000` and receive an S3-style XML error response; then run `docker compose stop` and confirm the container exits with status 0 well inside 10 seconds.

**Acceptance Scenarios**:

1. **Given** a clean checkout and a host with only Docker installed, **When** the operator runs `docker compose up`, **Then** the server builds, starts, logs the listen address and data directory it is using, and accepts connections on host port 9000.
2. **Given** the server is running under Compose, **When** the operator inspects its storage, **Then** the data directory is backed by a named Docker volume, not a bind mount.
3. **Given** the server is running, **When** any HTTP request arrives, **Then** the server answers with an S3 XML error (`Code` = `NotImplemented`, a `Message` and a `RequestId`) and HTTP status 501, because no S3 operation exists yet.
4. **Given** the server is idle, **When** it receives SIGTERM, **Then** it exits with status 0 in under 1 second.
5. **Given** a request is in progress, **When** the server receives SIGTERM, **Then** it refuses new connections, lets the in-progress request complete with its normal response, and exits with status 0 once it has.
6. **Given** a request is still in progress when the drain deadline passes (9 seconds after SIGTERM), **When** the deadline fires, **Then** the server closes the remaining connections, logs that shutdown was forced, and exits with a non-zero status within 10 seconds of SIGTERM, before Compose's stop grace period expires and Docker sends SIGKILL.

---

### User Story 2 - Point the server at a data directory and listen address (Priority: P2)

An operator runs the server outside the default Compose setup, or changes the Compose setup, and chooses where data lives and which address the server listens on.

**Why this priority**: The default Compose setup covers the common case; explicit configuration is what makes the server usable in tests (random ports, temp directories) and in other environments.

**Independent Test**: Start the server with a non-default listen address and a fresh data directory; confirm it listens there, uses that directory, and refuses to start when the directory is unusable or the address is taken.

**Acceptance Scenarios**:

1. **Given** the operator passes a listen address and a data directory at startup, **When** the server starts, **Then** it listens on that address and uses that directory, and logs both values.
2. **Given** the operator passes nothing, **When** the server starts, **Then** it uses the documented defaults (listen on port 9000 on all interfaces, data directory `/data`).
3. **Given** the data directory does not exist, **When** the server starts, **Then** it creates it.
4. **Given** the data directory exists but is not writable, or the path is a regular file, **When** the server starts, **Then** it exits with a non-zero status and a message naming the path and the reason, without listening.
5. **Given** the listen address is already in use or invalid, **When** the server starts, **Then** it exits with a non-zero status and a message naming the address and the reason.
6. **Given** an unknown or malformed startup option, **When** the server starts, **Then** it exits with a non-zero status and prints its usage.

---

### User Story 3 - Run tests, race tests, vet and the coverage gate without Go on the host (Priority: P3)

A developer on a Mac with only Docker installed runs `make test`, `make race`, `make vet` and `make cover`. Each command runs inside a container with a pinned toolchain version and reports pass or fail through its exit status.

**Why this priority**: The constitution makes `make test`, `make race` and `make cover` the merge gate for every pull request, and requires 100% statement coverage under `internal/`. This tooling must exist before the first S3 feature, but it delivers value only once there is code to test, so it ranks after the server itself.

**Independent Test**: On a Mac without Go, run each of the four targets and confirm each exits 0; then remove a test so one `internal/` package drops below 100% and confirm `make cover` exits non-zero and names that package.

**Acceptance Scenarios**:

1. **Given** a Mac (Apple Silicon or Intel) with Docker and no Go toolchain, **When** the developer runs `make test`, **Then** all unit tests run inside a container and the command exits 0 if they pass and non-zero if any fails.
2. **Given** the same host, **When** the developer runs `make race`, **Then** all tests run with the race detector and the command exits non-zero on any failure or detected race.
3. **Given** the same host, **When** the developer runs `make vet`, **Then** static analysis runs over all packages and the command exits non-zero on any finding.
4. **Given** every package under `internal/` has 100% statement coverage, **When** the developer runs `make cover`, **Then** it prints per-package coverage and exits 0.
5. **Given** at least one package under `internal/` is below 100% statement coverage, **When** the developer runs `make cover`, **Then** it exits non-zero and names each offending package with its coverage.
6. **Given** a package under `internal/` has no test files, **When** the developer runs `make cover`, **Then** that package counts as 0% and the gate fails.
7. **Given** a developer has run any target once, **When** they run it again with no changes, **Then** the toolchain image and dependency/build caches are reused rather than downloaded again.

---

### Edge Cases

- SIGINT (Ctrl-C in a foreground `docker compose up`) behaves the same as SIGTERM.
- A second SIGTERM or SIGINT during shutdown does not cut the drain short or crash the process; the 10-second deadline still applies.
- The server is the process that receives signals inside the container (not a shell wrapper that swallows them), so `docker compose stop` triggers graceful shutdown instead of a SIGKILL after the timeout.
- A long-lived idle keep-alive connection does not hold shutdown open; only requests actually in progress are waited for.
- Data in the named volume survives `docker compose down` followed by `docker compose up`; only `docker compose down -v` removes it.
- Host port 9000 is already taken: Compose reports the port conflict and the server does not start.
- The server runs as a non-root user in the container and can still write to the named volume.
- `cmd/s3d` is excluded from the 100% gate (constitution Principle IV); a package added under `internal/` later is picked up by the gate automatically without editing the Makefile.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The server MUST accept a listen address and a data directory at startup, with defaults of all interfaces on port 9000 and `/data`.
- **FR-002**: The server MUST create the data directory if it does not exist, and MUST refuse to start, with a non-zero exit status and an error naming the path, if the directory cannot be created or written.
- **FR-003**: The server MUST refuse to start, with a non-zero exit status and an error naming the address, if it cannot listen on the configured address.
- **FR-004**: The server MUST log its effective listen address and data directory at startup, and log the start and outcome (clean or forced) of shutdown.
- **FR-005**: Until S3 operations are implemented, the server MUST answer every request with HTTP 501 and an S3 XML error body containing `Code` (`NotImplemented`), `Message` and a unique `RequestId`.
- **FR-006**: On SIGTERM or SIGINT the server MUST stop accepting new connections and wait for in-progress requests to complete.
- **FR-007**: The server MUST exit within 10 seconds of the first SIGTERM or SIGINT. It MUST exit 0 when all in-progress requests completed, and non-zero when the deadline forced remaining connections closed.
- **FR-008**: `docker compose up` MUST build and start the server with no other setup step, publish it on host port 9000, and mount a named volume at the data directory.
- **FR-009**: The server process in the container MUST receive signals directly and MUST run as a non-root user.
- **FR-010**: `make test`, `make race`, `make vet`, `make cover` and `make e2e` MUST run inside a container with a pinned toolchain version and require nothing on the host except Docker and `make`.
- **FR-011**: Each `make` target MUST report success or failure through its exit status so it can be chained in scripts.
- **FR-012**: `make cover` MUST fail when any package under `internal/` is below 100% statement coverage or has no tests, and MUST name each failing package with its coverage.
- **FR-013**: `make cover` MUST discover packages under `internal/` automatically; adding a package MUST NOT require changing the gate.
- **FR-014**: The skeleton's own packages under `internal/` MUST meet the 100% coverage gate and pass `make race`.
- **FR-015**: The README MUST document how to start and stop the server, the startup options and their defaults, the `make` targets, and the trade-offs made in this feature (constitution Development Workflow).

### Key Entities

- **Server configuration**: listen address and data directory; each has a default and can be overridden at startup.
- **Data volume**: the named Docker volume that holds the data directory and outlives the container.
- **Coverage report**: per-package statement coverage for every package under `internal/`, compared against the 100% threshold.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a host with only Docker installed, an operator goes from a fresh clone to a server answering on port 9000 with exactly one command.
- **SC-002**: In 20 consecutive stop runs, the server exits in under 1 second when idle and in under 10 seconds when a request is in progress, and Docker never has to kill it.
- **SC-003**: An in-progress request that takes 3 seconds and is running when SIGTERM arrives completes with a full, successful response.
- **SC-004**: Each of the four developer commands runs to completion on a Mac with no Go toolchain installed, using one command each.
- **SC-005**: Lowering any `internal/` package to below 100% coverage (even a single uncovered statement) makes `make cover` fail, in every case tested.
- **SC-006**: A file written to the data directory before `docker compose down` is still there after `docker compose up`.
- **SC-007**: A repeat run of `make test` with no code changes downloads no toolchain or dependencies.

## Assumptions

- `make` is available on the host; it ships with the macOS Command Line Tools. "Only Docker installed" means no Go toolchain, not no `make`.
- Docker Desktop (or a compatible engine with Compose v2) is the supported runtime on macOS, on both Apple Silicon and Intel.
- The 10-second budget is measured from the first signal to process exit. Docker's default stop grace period is also 10 seconds, which would race the server's own deadline, so the Compose file sets a longer grace period (for example 15 seconds) to make sure the server's deadline always fires first.
- Startup options are passed as command-line flags; the Compose file sets them explicitly so the defaults are visible.
- No health-check endpoint is added: any path could be a valid bucket name in path-style S3 requests, so a reserved path would collide with S3 semantics. Readiness is checked by connecting to the port.
- The 501 `NotImplemented` response is a placeholder that S3 features will replace route by route.
- `make cover` measures statement coverage of `internal/` packages from their own tests; `cmd/s3d` stays thin and is covered by e2e tests in later features.
- No CI pipeline is added (constitution: accepted technical debt).
- No S3 operation, authentication or metadata format is defined here; those belong to later features.
