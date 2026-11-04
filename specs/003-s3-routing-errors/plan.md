# Implementation Plan: S3 Request Routing and Error Responses

**Branch**: `003-s3-routing-errors` | **Date**: 2026-11-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/003-s3-routing-errors/spec.md`

## Summary

Replace the 001 placeholder handler in `internal/s3api` with a path-style S3 router. An outer handler assigns
one request ID per request (header plus context), recovers panics into `500 InternalError`, and calls a router
that splits `r.URL.Path` (already decoded once by `net/http`) into bucket and key without cleaning it, detects
S3 subresources and CopyObject, and dispatches to one of eight `Op` fields in an `Ops` struct. Nil fields, and
every real S3 operation outside scope, answer `501 NotImplemented`; meaningless methods answer
`405 MethodNotAllowed`; malformed paths answer `400 InvalidURI`. `WriteError` becomes the single S3 error
renderer, reuses the request ID, and writes no body for `HEAD`. See [research.md](research.md) for each decision.

## Technical Context

**Language/Version**: Go 1.27.3 (`GOTOOLCHAIN=local`)

**Primary Dependencies**: Go standard library only (`net/http`, `net/url`, `encoding/xml`, `crypto/rand`, `log/slog`, `unicode/utf8`)

**Storage**: N/A

**Testing**: `go test` / `-race` / `go vet` / `scripts/cover-gate.sh` in the pinned golang container; `httptest` tables; one e2e test against the built binary

**Target Platform**: Linux containers (amd64 in CI, arm64 locally)

**Project Type**: Single Go service

**Performance Goals**: Routing is O(path length + query length) per request with no allocation beyond query parsing; no measurable target

**Constraints**: 100% statement coverage of `internal/`; no third-party modules; no request body reads in routing

**Scale/Scope**: `internal/s3api` (router, errors, request ID, recovery) and a two-line change in `internal/app`

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Status | How this plan complies |
|-----------|--------|------------------------|
| I. Durability Before Acknowledgement | N/A (pass) | No writes. Routing never reads a body, so no partial object can appear. |
| II. Streaming, Bounded Memory | Pass | Routing reads only the request line and headers. The writer wrapper keeps `Unwrap` so streaming handlers still reach `http.ResponseController` (R9). |
| III. Own Engine, Standard Library First | Pass | Standard library only. |
| IV. Test-First, 100% Coverage | Pass | Tests first in tasks; table tests cover every routing row, error, panic path and key case. No I/O in this feature, so `fsx` is not involved. `-race` clean. |
| V. Real-Client Compatibility | Pass | S3 XML errors with the S3 status and message for each code (contract). No operation is implemented, so no aws CLI e2e is required yet; quickstart covers the aws CLI manually. |
| Scope and Constraints | Pass | SigV4, auth, versioning, multipart stay out: their parameters route to 501 or are ignored. |
| Workflow: one feature, one PR | Pass | Branch `003-s3-routing-errors`. |
| Workflow: README design section | Pass | FR-014: trade-offs from R1, R2, R4, R5, R9 go into a `003` design subsection. |

Post-design re-check: no violations. Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/003-s3-routing-errors/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/http.md
├── checklists/requirements.md
└── tasks.md             # /speckit-tasks
```

### Source Code (repository root)

```text
internal/s3api/
├── errors.go          # APIError, ErrNotImplemented/MethodNotAllowed/InvalidURI/InternalError, WriteError (HEAD: no body)
├── errors_test.go
├── requestid.go       # newRequestID, context key, RequestID(ctx)
├── requestid_test.go
├── route.go           # Target, parseTarget, subresources set, route(r) decision
├── route_test.go      # routing table, key decoding table
├── handler.go         # Op, Ops, NewHandler(ops, log): request ID, writer wrapper, recover, dispatch
└── handler_test.go    # dispatch through the full handler, panics, HEAD bodies, request-ID uniqueness
internal/app/
├── app.go             # run takes func(*slog.Logger) http.Handler; Main passes s3api.NewHandler(s3api.Ops{}, l)
└── app_test.go
e2e/routing_test.go    # //go:build e2e; built binary: 501, 405, HEAD without body, x-amz-request-id
README.md              # intro and quick start wording; design section "003 S3 routing and errors"
```

**Structure Decision**: Everything stays in `internal/s3api`, split by concern into files of one package.
Dependency direction is unchanged: `app → s3api`; `s3api` imports only the standard library.

## Key implementation notes

- **Order matters**: the request-ID header is set before routing, so every outcome, including `405` and panics,
  carries it.
- **Recovery**: `defer` in the outer handler; `recover()` value `http.ErrAbortHandler` is re-panicked without
  logging; others are logged, then `WriteError(ErrInternalError)` or `panic(http.ErrAbortHandler)` depending on
  whether the wrapper saw `WriteHeader`/`Write`.
- **Tests for panic-after-write** use `httptest.NewServer` and check the client gets a transport error, since
  `httptest.ResponseRecorder` cannot show an aborted connection.
- **Request ID in `WriteError`** falls back to a fresh ID when the context has none, so `WriteError` stays
  usable and testable on its own.
- **`app` change**: `run(..., newHandler func(*slog.Logger) http.Handler)`; existing tests pass a function that
  ignores the logger.

## Complexity Tracking

No violations to justify.
