# Research: Durable Object Storage

All decisions below were taken autonomously with the recommended option, as authorized by the feature owner for this feature.

## R1 — On-disk layout

- **Decision**: `<root>/tmp/`, `<root>/data/<version>`, `<root>/meta/<hex sha256(bucket + "/" + key)>.json`. A version is `crypto/rand.Text()` (26 base32 characters).
- **Rationale**: Hashing the key makes every key, including `..`, `/` and non-ASCII, map to a fixed-length safe file name inside `meta/` (FR-011). Unique, immutable data files mean an overwrite never touches bytes an open reader holds (FR-008). `tmp/` lives under the same root, so every rename stays on one filesystem.
- **Alternatives**: Key-derived directory trees (path traversal risk, deep trees, case-folding issues on some filesystems); a single file with data plus a metadata trailer (one rename commits, but it reorders the constitution's "rename then metadata record" sequence and needs a deviation).

## R2 — Commit order and commit point

- **Decision**: Body temp → `Sync` → `Close` → `Rename` to `data/<version>` → sync `data/` → record temp → `Sync` → `Close` → lock stripe → read previous record → `Rename` record into `meta/` → sync `meta/` → remove previous data file → unlock.
- **Rationale**: Matches constitution Principle I literally; the record rename is the commit point. Writing the record temp outside the lock keeps the critical section to a rename, a directory sync and a remove.
- **Alternatives**: Holding the lock for the whole Put (serializes uploads to one key behind slow clients); skipping the `data/` sync (a crash could leave a committed record pointing at a missing body).

## R3 — Failure after the commit rename

- **Decision**: If syncing `meta/` fails after the rename, roll back: re-commit the previous record through the same temp/sync/rename/sync path, or remove the new record when there was none; then remove the new data file. Return the original error joined with any rollback error. If removing the previous data file fails after a successful commit, Put still succeeds.
- **Rationale**: The spec requires that no new version becomes visible after a failed Put. The previous data file is removed only after a successful `meta/` sync, so it is still there for the rollback. If the rollback itself fails, both versions are complete, so no partial object is exposed.
- **Alternatives**: Returning an error and leaving the new record visible (violates FR-006); retrying the sync (hides a failing disk).

## R4 — Concurrency

- **Decision**: 64 `sync.Mutex` stripes chosen by the first byte of the record hash. Commit, Delete, and Get's record read plus data open run under the stripe. Head reads the record without the lock.
- **Rationale**: Without the lock, two commits could both read the same previous record and orphan one loser's data, and a Get could read a record whose data file a concurrent commit has just removed. Stripes bound memory regardless of key count. A single process owns the data directory (constitution scope), so in-process locks are enough.
- **Alternatives**: A per-key lock map with reference counts (more code, same guarantee); lock-free Get with retry on `ENOENT` (harder to test deterministically).

## R5 — Streaming, length and digest checks

- **Decision**: `io.CopyBuffer(io.MultiWriter(file, md5), io.LimitReader(ctxReader, size), buf)` with a 32 KiB buffer, then read one more byte. Fewer than `size` bytes or any extra byte → `ErrIncompleteBody`. A supplied digest that differs → `ErrBadDigest`. `size < 0` or invalid key → `ErrInvalidArgument`; `size > 5 GiB` → `ErrEntityTooLarge`; key over 1024 bytes → `ErrKeyTooLong`. A short write without an error is reported as `io.ErrShortWrite` by `io.MultiWriter`.
- **Rationale**: The multi-writer hides the file's `ReaderFrom`, so the copy always uses the fixed buffer (FR-002). The probe byte detects a body longer than declared without buffering it.
- **Alternatives**: `io.CopyN` (allocates its own buffer, and `*os.File.ReadFrom` could bypass fault-injected writes).

## R6 — Directory fan-out

- **Decision**: Flat `data/` and `meta/`.
- **Rationale**: ext4 and XFS handle millions of entries with hashed directory indexes; tiny-s3 is single-node and small. Fan-out can be added later without changing the record format, since the record names its data version.
- **Alternatives**: Two-level hex fan-out (more directory syncs per Put, more code now).

## R7 — Crash leftovers

- **Decision**: `Open` creates `tmp/`, `data/`, `meta/` and removes every entry in `tmp/`. Orphaned files in `data/` (crash between the data rename and the record commit, or a failed best-effort removal) are left in place.
- **Rationale**: `tmp/` entries are never referenced, so removing them is always safe. Finding orphaned data requires reading every record, which is the index-rebuild feature's job.
- **Alternatives**: Full orphan sweep at `Open` (duplicates the future index rebuild).

## R8 — fsx read-side operations

- **Decision**: Add `FS.Open(path) (ReadFile, error)` and `FS.ReadDir(path) ([]fs.DirEntry, error)`; `ReadFile` is `Name`, `Read`, `ReadAt`, `Seek`, `Close`. `Faulty` adds `OpOpen`, `OpRead` (applies to `Read` and `ReadAt`) and `OpReadDir`; `Close` on a read file uses the existing `OpClose`.
- **Rationale**: Constitution Principle IV requires I/O failures to be tested through `fsx`; reading records and listing `tmp/` have error branches the coverage gate must reach.
- **Alternatives**: Calling `os` directly for reads (untestable branches, breaks the single seam).

## R9 — Wiring

- **Decision**: Do not wire the store into `internal/app` in this feature.
- **Rationale**: The spec is HTTP-independent and adds no endpoint; wiring belongs with PutObject so the startup behavior and its e2e test land together.
- **Alternatives**: Calling `storage.Open` at startup now (adds startup behavior nothing uses yet).

## R10 — Metadata record format

- **Decision**: JSON with fields `v` (1), `bucket`, `key`, `size`, `md5`, `last_modified` (RFC 3339 nanoseconds, UTC) and `data` (version). Reads are capped at 64 KiB; undecodable records or `v != 1` → `ErrCorruptRecord`.
- **Rationale**: Readable on disk, standard library, versioned so content type and user metadata can be added without migration.
- **Alternatives**: Binary encoding (no benefit at this size).

## R11 — Clock

- **Decision**: `Store.now` defaults to `time.Now`; tests in package `storage` replace it. Last-modified is `now().UTC()` taken just before the record is encoded.
- **Rationale**: Deterministic tests without an exported option.
