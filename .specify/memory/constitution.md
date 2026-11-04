# tiny-s3 Constitution

tiny-s3 is a minimal S3-compatible object store written in Go.

## Core Principles

### I. Durability Before Acknowledgement (NON-NEGOTIABLE)

- A PutObject MUST NOT return success until, in order: the body is written to a temp file on
  the same filesystem as its final location, the temp file is fsynced, it is atomically renamed
  into place, the parent directory is fsynced, and the metadata record is durably written.
- The durable metadata record is the commit point. An object without it does not exist.
- A partially written object MUST never be visible to GET, HEAD or LIST, including after a
  crash at any step of the write path.
- Any failure on the write path MUST remove the temp file and return an S3 error response.

Rationale: a storage system that acknowledges data it can lose is worse than one that fails
loudly. Every step above closes a specific crash window.

### II. Streaming, Bounded Memory

- Object bodies MUST be streamed between the network and disk. No code path may buffer a
  whole object in memory.
- Memory used per request MUST be bounded and independent of object size.

Rationale: object size is client-controlled; buffering turns any large upload into an
out-of-memory kill.

### III. Own Storage Engine, Standard Library First

- The storage engine, the index and the S3 protocol layer MUST be written in this repository
  using the Go standard library.
- No open-source S3-compatible system (or its packages) may be imported.
- Third-party modules are allowed only in end-to-end tests (AWS SDK for Go v2).

Rationale: the point of the project is to own the engine; a small dependency surface keeps it
auditable and the build reproducible.

### IV. Test-First, 100% Coverage of the Core (NON-NEGOTIABLE)

- Tests are written before the code they cover.
- Every package under `internal/` MUST have 100% statement coverage; `make cover` MUST fail
  below that threshold.
- I/O failures (create, write, sync, rename, remove, directory sync) MUST be tested through
  the `internal/fsx` fault-injection layer, not by mocking at call sites.
- `cmd/s3d` MUST stay thin (wiring and configuration only) and is covered by e2e tests.
- All tests MUST pass with `-race`.

Rationale: the durability guarantees in Principle I are only as good as the tests that prove
each failure branch cleans up and reports correctly.

### V. Real-Client Compatibility

- Every implemented S3 operation MUST have at least one e2e test driven by the real
  `aws s3api` CLI v2.
- Errors MUST use the S3 XML error format (`Code`, `Message`, `RequestId`) with the HTTP
  status S3 would return for the same condition.
- Single-part ETags MUST be the quoted hex MD5 of the object body.

Rationale: compatibility is defined by what real clients accept, not by our reading of the
documentation.

## Scope and Constraints

- In scope: PutObject, GetObject with a single Range, HeadObject, DeleteObject,
  ListObjectsV2 (prefix, delimiter, pagination), minimal bucket operations, and aws-chunked
  request body decoding.
- Out of scope: SigV4 verification, authentication, versioning, ACLs, multi-node operation,
  and multipart upload. The multipart upload design is documented in the README only.
- Everything runs in Docker. Object data lives in a named Docker volume, not a bind mount.
- The index is held in memory and rebuilt from the durable metadata records at startup.
- Accepted technical debt: Prometheus metrics are not provided. Adding them is a scope change,
  not a fix.

## Development Workflow

- Each Spec Kit feature is delivered in exactly one pull request, on its own branch.
- A pull request is mergeable only when the required CI checks `make vet`, `make test`,
  `make race`, `make cover` and `docker build` pass. CI runs the same Makefile targets in the
  same Go container that developers run locally, so a failing check is reproduced with the
  command it is named after.
- Every relevant trade-off made in a pull request MUST be recorded in the README design
  section in that same pull request.

## Governance

- This constitution supersedes ad-hoc preferences and conventions. When they conflict, the
  constitution wins.
- Amendments are made by pull request and MUST bump the version using semantic versioning:
  MAJOR when a principle is removed or redefined, MINOR when a principle or section is added
  or materially expanded, PATCH for wording and clarifications.
- Every plan MUST check compliance with these principles. Any deviation MUST be justified in
  the plan's Complexity Tracking section; an unjustified deviation blocks the pull request.

**Version**: 1.1.0 | **Ratified**: 2026-11-04 | **Last Amended**: 2026-11-04
