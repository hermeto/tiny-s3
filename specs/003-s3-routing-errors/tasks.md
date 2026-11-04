---

description: "Task list for S3 request routing and error responses"
---

# Tasks: S3 Request Routing and Error Responses

**Input**: Design documents from `/specs/003-s3-routing-errors/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/http.md, quickstart.md

**Tests**: Required. The constitution (Principle IV) mandates test-first and 100% statement coverage of
`internal/`. Each story writes its tests first, sees them fail, then implements.

**Organization**: Tasks are grouped by user story. All code is in `internal/s3api` (one package, several files),
so [P] marks only tasks touching different files with no dependency on incomplete work.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1 = dispatch, US2 = S3 errors, US3 = request IDs, US4 = key decoding
- Run every `make` target from the repository root; all run in the pinned golang container.

---

## Phase 1: Setup

- [X] T001 Create branch `003-s3-routing-errors` from an up-to-date `main` in the repository root

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Error values, request ID and the new handler signature every story builds on.

- [X] T002 [P] Write tests in `internal/s3api/requestid_test.go`: `newRequestID` matches `^[0-9A-F]{16}$` and two calls differ; `RequestID(ctx)` returns `""` for a bare context and the stored value after `withRequestID`
- [X] T003 [P] Write tests in `internal/s3api/errors_test.go` for `WriteError(w, r, e APIError)`: status, `Content-Type: application/xml`, body is `xml.Header` + `<Error>` with `Code`, `Message`, `Resource`, `RequestId` in that order; `Resource` is `r.URL.Path` XML-escaped; with a request ID in the context, header and body use it; without, a fresh ID is used in both; for `HEAD` the body is empty but status and headers are set. Assert the four `APIError` values exactly as contracts/http.md: `NotImplemented` 501 `A header or method you provided implies functionality that is not implemented.`, `MethodNotAllowed` 405 `The specified method is not allowed against this resource.`, `InvalidURI` 400 `Couldn't parse the specified URI.`, `InternalError` 500 `We encountered an internal error. Please try again.`
- [X] T004 Implement `internal/s3api/requestid.go` (`newRequestID`, unexported context key, `withRequestID`, exported `RequestID(ctx) string`) and rewrite `internal/s3api/errors.go` (`APIError`, the four values, `WriteError` per research.md R8, moving `newRequestID` out) until T002 and T003 pass
- [X] T005 Change `internal/app/app.go`: `run` takes `newHandler func(*slog.Logger) http.Handler` and calls it with the logger it builds; `Main` passes `func(l *slog.Logger) http.Handler { return s3api.NewHandler(s3api.Ops{}, l) }`. Update `internal/app/app_test.go` callers. Give `internal/s3api/handler.go` the `Target`, `Op`, `Ops` types and the `NewHandler(ops Ops, log *slog.Logger)` signature (still answering `ErrNotImplemented` for everything) so the build stays green

**Checkpoint**: `make test` passes; every request still gets 501.

---

## Phase 3: User Story 1 - Each S3 request reaches the right operation (P1) 🎯 MVP

**Goal**: The eight operations reach their own `Ops` field with the right `Target`.

**Independent Test**: Recorder in every `Ops` field; each request in the table calls exactly the expected field once with the expected bucket and key, and no look-alike calls any field.

- [X] T006 [US1] Write a table test in `internal/s3api/handler_test.go` through `NewHandler` with a recorder per `Ops` field: rows 4, 7, 9, 10, 13–16 of contracts/http.md (`GET /`, `GET /b?list-type=2`, `PUT /b`, `PUT /b/`, `HEAD /b`, `PUT|GET|HEAD|DELETE /b/k`), plus `GET /b?list-type=2&prefix=a&delimiter=/&max-keys=5&continuation-token=t&start-after=s&fetch-owner=true&encoding-type=url`, `GET /b/k?response-content-type=text/plain&x-id=GetObject`, `PUT /b/k?x-id=PutObject` and a presigned-style `GET /b/k?X-Amz-Signature=abc`. Assert the field called and its `Target`
- [X] T007 [US1] Add a test in `internal/s3api/handler_test.go` that `NewHandler(Ops{}, log)` answers `501 NotImplemented` for all eight in-scope requests (FR-007)
- [X] T008 [US1] Implement `internal/s3api/route.go`: `parseTarget(path) (Target, bool)` per research.md R3, the subresource set from contracts/http.md verbatim, and `route(r) (Op-selector, APIError)` in the order of research.md R4; wire it into `NewHandler` in `internal/s3api/handler.go`, a nil `Op` answering `ErrNotImplemented`. T006 and T007 pass

**Checkpoint**: Dispatch works with recorders.

---

## Phase 4: User Story 2 - Every failure is a readable S3 error (P1)

**Goal**: 501, 405, 400 and 500 follow the contract; HEAD errors have no body.

**Independent Test**: Status, code, XML shape, `RequestId` = header, HEAD empty body, panic recovered.

- [X] T009 [US2] Write a table test in `internal/s3api/handler_test.go` (recorder `Ops` that fail the test if called): 405 for `PATCH`, `OPTIONS`, `TRACE`, `FOO` on `/`, `/b`, `/b/k` and for `PUT|POST|DELETE|HEAD /`; 501 for `GET /b`, `GET /b?list-type=1`, `GET /b?acl`, `GET /b?list-type=2&acl`, `DELETE /b`, `POST /b`, `POST /b?delete`, `PUT /b/k?tagging`, `GET /b/k?uploadId=x`, `GET /b/k?versionId=1`, `GET /b/k?partNumber=1`, `POST /b/k?uploads`, `HEAD /b?acl`, `PUT /b/k` with `x-amz-copy-source`, `GET /b?a=%zz&acl`; not 501 for `GET /b/k?ACL` (case-sensitive) and `GET /b?a=%zz&list-type=2`; 400 `InvalidURI` for `//`, `//k`, `/b/%FF` and `/%FF`. Non-HEAD rows parse as S3 XML with `RequestId` equal to `x-amz-request-id`; HEAD rows have an empty body and the header
- [X] T010 [US2] Write panic tests in `internal/s3api/handler_test.go`: an `Op` that panics before writing gives `500 InternalError` XML and a `handler_panic` JSON log line with `request_id`, `method`, `path`, `panic`; an `Op` that writes the header then panics aborts the connection (client transport error, via `httptest.NewServer`) and logs; an `Op` that panics with `http.ErrAbortHandler` aborts and does not log; after each, the next request on the same server succeeds. Test `Unwrap` returns the underlying writer (via `http.NewResponseController(w).Flush()` inside an `Op` succeeding)
- [X] T011 [US2] Implement the writer wrapper (`WriteHeader`, `Write`, `Unwrap`) and the deferred recovery in `internal/s3api/handler.go` per research.md R9 until T009 and T010 pass

---

## Phase 5: User Story 3 - Every response can be traced by request ID (P2)

**Goal**: One ID per request on every response.

**Independent Test**: 1,000 mixed requests, header always present and unique; an `Op` that calls `WriteError` emits the header ID in the body.

- [X] T012 [US3] Write tests in `internal/s3api/handler_test.go`: an `Op` that succeeds (`204`) still carries `x-amz-request-id`; an `Op` that reads `RequestID(r.Context())` sees the header value; an `Op` that calls `WriteError(w, r, ErrNotImplemented)` produces body `RequestId` equal to the header; 1,000 requests cycling through a success, a 501, a 405, a 400 and a HEAD each carry a header matching `^[0-9A-F]{16}$` with no repeats (SC-004)
- [X] T013 [US3] Make the outer handler in `internal/s3api/handler.go` generate the ID, set the header and store it in the context before routing, until T012 passes

---

## Phase 6: User Story 4 - Keys with any characters reach the handler intact (P1)

**Goal**: Bucket and key decoded exactly once, never cleaned.

**Independent Test**: At least 20 keys, encoded as the AWS SDKs do, arrive byte-identical.

- [X] T014 [US4] Write a table test in `internal/s3api/route_test.go` sending real requests through `httptest.NewServer` (so `net/http` does the parsing) to a recorder `GetObject`: `a/b/c`, `with space`, `relatório/ação.txt`, `日本語/キー`, `emoji-😀`, `100%`, `%41` (sent `%2541`), `a+b`, `a%2Fb` → `a/b`, `#hash` (sent `%23hash`), `q?mark` (sent `q%3Fmark`), `dir/`, `a//b`, `./a`, `a/../b`, `../x`, `trailing.`, `semi;colon`, `amp&er=sand`, `tilde~`, `quote"'`, `<angle>`; bucket `my.bucket-1` decoded the same way. Also check the 501 error `Resource` for one encoded key is `/b/<decoded key>`, and that an absolute-form request line (`GET http://host/b/a%20b HTTP/1.1`, written on a raw `net.Conn`) routes to `GetObject` with key `a b` (FR-012, SC-005)
- [X] T015 [US4] Fix `internal/s3api/route.go` until T014 passes; there must be no call to `url.PathUnescape`/`url.QueryUnescape` on the path

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T016 [P] Add `e2e/routing_test.go` (`//go:build e2e`) using the existing `TestMain` binary helpers: start `s3d`, check `GET /` → 501 XML with `RequestId` = header, `PATCH /b/k` → 405, `HEAD /b/k` → 501 with empty body and the header; stop with SIGTERM
- [X] T017 [P] Update `README.md`: intro and quick start describe routing (501 for unbuilt operations, 405, 400), and add a `### 003 S3 routing and errors` design subsection with the trade-offs from research.md R1, R2, R4, R5, R9 and the plain-text 400 for malformed targets (FR-014)
- [X] T018 Run `make vet test race cover e2e`; all exit 0 and `make cover` shows 100% for every `internal/` package. Record output
- [X] T019 Run the quickstart curl and aws CLI checks against `docker compose up -d --build`; record results

---

## Dependencies & Execution Order

- Setup → Foundational (T002 ∥ T003 → T004 → T005) → US1 → US2 → US3 → US4 → Polish.
- US2 to US4 touch the same `handler.go`/`route.go` as US1, so they run after it; T016 and T017 can run in parallel with each other once US4 is done.

## Parallel Example

```text
T002 requestid_test.go  ∥  T003 errors_test.go
T016 e2e/routing_test.go  ∥  T017 README.md
```

## Implementation Strategy

MVP is Foundational + US1: dispatch works, everything else still answers 501. US2 to US4 harden it; Polish
documents and validates. One commit per logical step (Conventional Commits), one PR for the feature.
