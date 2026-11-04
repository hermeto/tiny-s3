# Data Model: Server Skeleton

This feature stores no objects. The entities below are the runtime values the skeleton defines and that later
features build on.

## Config (`internal/config`)

| Field | Type | Source | Default | Validation |
|-------|------|--------|---------|------------|
| `Listen` | string | `-listen` flag | `:9000` | Non-empty. Resolved by `net.Listen("tcp", …)`; failure is a startup error (exit 1). |
| `DataDir` | string | `-data-dir` flag | `/data` | Non-empty. Cleaned with `filepath.Clean`. Checked by the data directory probe below. |
| `ShutdownTimeout` | duration | constant, not a flag | 9 s | Set by `Parse`; tests construct `Config` directly with a short value. |

Parse rules: unknown flags, malformed values, empty values and positional arguments return a usage error
(exit 2, usage printed to stderr). `-h` / `-help` prints usage and exits 0.

## Data directory (`internal/app`, via `internal/fsx`)

Startup sequence, each step through the `fsx.FS` interface:

1. `MkdirAll(DataDir, 0o750)`. Error → "data dir {path}: {err}", exit 1. `MkdirAll` returns nil only when the path
   is (or becomes) a directory; an existing regular file fails with `mkdir {path}: not a directory` (ENOTDIR), so no
   separate `Stat` check is needed.
2. `CreateTemp(DataDir, ".probe-*")`, `Close`, `Remove`. Any error → "data dir {path} not writable: {err}", exit 1.
   The probe file is removed on every path where it was created.

No files are left in the data directory by this feature.

## Server lifecycle (`internal/server`)

```text
            Run(ctx, ln)
                │
                ▼
          ┌──────────┐   Serve returns error (not ErrServerClosed)
          │ serving  │ ───────────────────────────────────────────► return err          (exit 1)
          └──────────┘
                │ ctx cancelled (SIGTERM / SIGINT)
                ▼
          ┌──────────┐   all active requests finish before deadline
          │ draining │ ───────────────────────────────────────────► return nil          (exit 0)
          └──────────┘
                │ ShutdownTimeout elapsed
                ▼
          ┌──────────┐
          │ forced   │ ── Close() all connections ───────────────► return ErrForcedShutdown (exit 1)
          └──────────┘
```

- In `draining`, the listener is closed (new connections are refused) and idle keep-alive connections are
  closed immediately; only active requests are waited for.
- Further signals during `draining` have no effect.

## S3 error (`internal/s3api`)

| Element | Value in this feature |
|---------|-----------------------|
| `Code` | `NotImplemented` |
| `Message` | `A header or method you provided implies functionality that is not implemented.` |
| `Resource` | Request path (escaped by the XML encoder) |
| `RequestId` | 16 uppercase hex characters, unique per request; also sent as `x-amz-request-id` |

HTTP status 501, `Content-Type: application/xml`. See [contracts/http.md](contracts/http.md).

## fsx.FS (`internal/fsx`)

| Operation | Signature (shape) | Faulty wrapper |
|-----------|-------------------|----------------|
| `MkdirAll` | `(path, perm) error` | returns injected error when op = `mkdirall` |
| `CreateTemp` | `(dir, pattern) (File, error)` | op = `createtemp` |
| `File.Close` | `() error` | op = `close` |
| `Remove` | `(path) error` | op = `remove` |

Injected errors can match an operation and, optionally, a path prefix, so a test can fail exactly one call.
