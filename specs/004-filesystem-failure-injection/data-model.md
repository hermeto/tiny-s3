# Data Model: Filesystem Failure Injection

## Filesystem (`fsx.FS`)

The dependency consumed by storage code. It exposes the supported filesystem operations and returns narrow file or directory handles. `fsx.OS` is the production implementation; `fsx.Faulty` decorates another `FS` for tests.

## File Handle

- `Name`: stable path used for cleanup and history.
- `Write`: writes bytes and returns `(count, error)`.
- `Sync`: makes file contents durable.
- `Close`: releases the handle.

The OS implementation delegates each method to the corresponding `*os.File` method. A fake handle delegates normally unless a matching fault changes the result.

## Directory Handle

- `Name`: opened directory path.
- `Sync`: makes directory entry changes durable.
- `Close`: releases the handle.

Directory handles are opened through the abstraction so directory-open and directory-sync failures are injectable and visible in history.

## Fault Rule

- `Operation`: one supported operation kind.
- `PathPattern`: glob pattern applied to the operation's relevant path.
- `Nth`: positive matching-call number at which to inject.
- `Err`: error returned by the injected operation, or a short-write result for a write rule.
- `WriteCount` (write rules only): optional count below the requested length.

Rules are evaluated in registration order. A rule consumes its configured matching-call position only for calls with the same operation and matching path. Invalid glob patterns are rejected at construction time.

## Operation Record

- `Operation`: attempted operation kind.
- `Path`: primary path.
- `OtherPath`: second path for rename, when applicable.
- `Bytes`: requested and returned byte counts for writes.
- `Err`: final result, nil for success.

Records are append-only and ordered by attempted call. The history snapshot is copied before returning to callers so assertions cannot mutate fake state.

## State Transitions

1. A caller constructs `Faulty` with an underlying filesystem and validated rules.
2. Each operation is matched, recorded, injected or delegated, and finalized with its result.
3. An opened handle remains usable until close; its later writes/sync/close are recorded against its name.
4. Rule counters and history persist for the life of the fake, allowing Nth-call assertions across a complete storage operation.
