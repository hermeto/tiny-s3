# Research: Minimal Bucket Support

Every decision below took the recommended option, as authorized by the feature owner for this feature.

## R1 Where buckets live

- **Decision**: In `internal/storage`, beside objects. A bucket is a durable record `buckets/<name>.json` under the store root, `{"v":1,"name":"demo","created":"<RFC 3339 UTC>"}`. The engine keeps every bucket in an in-memory map loaded by `Open`.
- **Rationale**: One store root, one `Open`, one `tmp/` cleanup. The constitution already says the index lives in memory and is rebuilt from durable records at startup; buckets are the first index to do so. The name is validated before it becomes a file name, and valid names contain only `[a-z0-9.-]`, never `/` and never `..`, so no name can address a path outside `buckets/`.
- **Alternatives considered**: Hashed file names as in `meta/` (needless: the name is validated, and readable names help operators). A single `buckets.json` rewritten on every create (rewrites all buckets per create and serializes on one file). A directory per bucket (couples the bucket to the object layout, which is flat by hash).

## R2 Create write path

- **Decision**: Validate the name; take the bucket write lock; fail with `ErrBucketExists` if the map has the name; write the record to `tmp/bucket-*`, sync, close, rename to `buckets/<name>.json`, sync `buckets/`; only then add it to the map and return. A failure before the rename removes the temp file. A failure of the `buckets/` sync after the rename removes the record and syncs again (rollback); if the rollback also fails, the bucket is added to the map (the record is on disk and complete) and both errors are returned.
- **Rationale**: Same order and same commit point as Principle I for objects: the renamed and synced record is the bucket. The rollback mirrors object Put (005 R3), so a failed create never leaves the bucket visible unless the disk state forces it, and then it is complete.
- **Alternatives considered**: `O_EXCL` link instead of rename (fsx has no link operation; the in-process lock already serializes creates, since one s3d owns a data directory).

## R3 Concurrency

- **Decision**: One `sync.RWMutex` for the bucket map. Create holds the write lock across the whole write path; HeadBucket and ListBuckets take the read lock.
- **Rationale**: Creates are rare, so serializing them costs nothing and guarantees exactly one winner among concurrent creates (SC-005). Reads stay concurrent.
- **Alternatives considered**: Per-name stripes as for object keys (more code for an operation that is not hot).

## R4 Startup load

- **Decision**: `Open` lists `buckets/` and decodes every entry. An entry whose name is not `<valid bucket name>.json`, that is over 64 KiB, does not decode, has an unknown version, a name different from its file name, or a zero creation date fails `Open` with `ErrCorruptRecord` naming the path. An I/O error listing `buckets/` or opening or reading an entry fails `Open` with that error, also naming the path. `s3d` then exits 1 with `s3d: <error>`.
- **Rationale**: Clarification 5 (refuse to start). The write path never leaves a partial record in `buckets/` (temp + rename), so a bad entry means outside interference, which the operator must see.
- **Alternatives considered**: Skip and log (a bucket silently disappears; its objects become unreachable).

## R5 Name validation

- **Decision**: `storage.ValidBucketName` implements clarification 1: 3–63 bytes; `[a-z0-9.-]`; first and last character a letter or digit; no `..`; not four dot-separated decimal numbers (IPv4 form); no prefix `xn--`, `sthree-`, `amzn-s3-demo-`; no suffix `-s3alias`, `--ol-s3`, `.mrap`, `--x-s3`, `--table-s3`. `CreateBucket` and `HeadBucket` return `ErrInvalidBucketName` for an invalid name before any lookup or disk access.
- **Rationale**: The rules are the S3 general purpose bucket rules; validating in the engine keeps the file-name safety argument of R1 local to the package that relies on it.
- **Alternatives considered**: Validating only in `s3api` (the engine would trust its caller for path safety).

## R6 The NoSuchBucket gate

- **Decision**: `s3api.Ops` gains `CheckBucket func(r *http.Request, bucket string) (APIError, bool)`; the request lets an internal error be logged with its request ID. After routing succeeds for a request with a bucket, and unless it is a bucket-level `PUT` (CreateBucket), the handler calls `CheckBucket`; when it returns false, the handler answers that error and no Op runs. The check runs before the "nil Op is NotImplemented" fallback, so object operations that are not implemented yet still answer `NoSuchBucket` on a missing bucket and `NotImplemented` on an existing one. `HeadBucket`'s Op then only answers 200. Requests stopped by routing (subresources, unsupported methods, `InvalidURI`) are unchanged.
- **Rationale**: One gate covers HeadBucket and all five object operations, today and when the object features land. The router (003) is unchanged; the gate is skipped when `CheckBucket` is nil, so existing router tests stand.
- **Alternatives considered**: A check inside every Op (each future Op must remember it, and nil Ops could not answer `NoSuchBucket`). A bucket check in `storage.Put/Get/Head/Delete` (the 005 engine and its tests treat the bucket as a namespace; the HTTP gate is enough while one process owns the store and DeleteBucket does not exist).

## R7 HTTP behavior

- **Decision**:
  - CreateBucket: read at most 64 KiB of body. Empty (after trimming whitespace) means us-east-1. Otherwise decode `CreateBucketConfiguration`; a decode error, a read error or a body over 64 KiB is `400 MalformedXML`. `LocationConstraint` absent, empty or `us-east-1` is accepted; any other value is `400 InvalidLocationConstraint`. Success is `200` with `Location: /<name>` and no body.
  - HeadBucket: `200` with `x-amz-bucket-region: us-east-1`, no body.
  - ListBuckets: `200`, `application/xml`, `ListAllMyBucketsResult` in the S3 namespace with a fixed owner (`ID` and `DisplayName` `tiny-s3`), an always-present `<Buckets>` element, buckets sorted by name, `CreationDate` in UTC with millisecond precision (`2006-01-02T15:04:05.000Z`). Query parameters are ignored.
  - Error mapping: `ErrInvalidBucketName` → `400 InvalidBucketName`; `ErrNoSuchBucket` → `404 NoSuchBucket`; `ErrBucketExists` → `409 BucketAlreadyOwnedByYou`; any other error → `500 InternalError`, logged as `bucket_error` with the request ID, bucket and error.
- **Rationale**: Matches what S3 sends and what the aws CLI parses. Accepting an explicit `us-east-1` is more lenient than S3 (which rejects it) and harmless. A real S3 in us-east-1 returns 200 for re-creating an owned bucket; the spec asks for 409, which every other region returns.
- **Alternatives considered**: Ignoring the body (a client asking for eu-west-1 would silently get us-east-1).

## R8 Wiring

- **Decision**: `app.run` opens the store with `storage.Open(fsys, cfg.DataDir)` after the data-dir check and before listening; a failure prints `s3d: <error>` and exits 1. `app.Main` builds the handler from `s3api.BucketOps(store, log)`.
- **Rationale**: Startup failures stay loud and happen before the port opens, so a server that listens has loaded every bucket.

## R9 Real-client e2e

- **Decision**: `make e2e` runs in a small image built from `e2e/Dockerfile`: `amazon/aws-cli:2.31.11` pinned by index digest, plus `/usr/local/go` copied from the Makefile's pinned golang image (passed as a build argument). The e2e tests run the real `aws s3api` binary against the s3d binary they build, with throwaway credentials, `--endpoint-url` and a temporary `HOME`. `aws` missing from `PATH` fails the test rather than skipping it.
- **Rationale**: Principle V requires `aws s3api` CLI v2. The amazon image is the official CLI v2 build; copying the Go tree keeps one toolchain definition (the same digest as every other target). A probe build ran `go version` (1.27.3) and `aws --version` (2.31.11) in the image.
- **Alternatives considered**: Installing the CLI zip in the golang image (needs a checksum pin per architecture and a network download in every cache-cold build). Running the CLI on the host (not reproducible, breaks "Docker and make only").
- **Note**: CI does not run `make e2e` today (002 contract); adding it means a new required check and a ruleset change, out of scope here.
