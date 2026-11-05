# Research: DeleteObject

Every decision took the recommended option, as authorized by the feature owner.

## R1 Wiring

- **Decision**: `ObjectStore` gains `Delete(bucket, key) error` (the 005 engine method); `DeleteObjectOp` answers 204 on success and maps errors through the shared table (`KeyTooLongError`, `InvalidArgument`, 500 `InternalError` logged as `storage_error`). It is wired in `app.Main`.
- **Rationale**: The engine already gives every guarantee in the spec: it removes the record (the commit point) and syncs `meta/` before returning, keeps the data file when that sync fails (so a record that reappears still points at a full body), removes a corrupt record, and never touches readers that already opened the data file (POSIX keeps unlinked open files readable).

## R2 Proving durability after a crash

- **Decision**: An e2e test uploads, deletes with the aws CLI, sends SIGKILL to s3d right after the CLI returns, restarts it on the same data directory and expects `head-object` 404 and `get-object` `NoSuchKey`. Handler tests with `fsx.Faulty`: a failing record removal expects 500 and a readable object; a failing `meta/` sync after the removal expects 500 and the data file kept (found during implementation: the first draft of the spec said the object stays readable in both cases, which the engine does not and cannot do once the record is removed).
- **Note**: SIGKILL does not drop the OS page cache, so it proves the acknowledged state is on the filesystem, not that it survived power loss; the engine's sync-order tests (005) cover the fsync calls.

## R3 Delete during a download

- **Decision**: The 009 e2e test `TestGetSurvivesOverwrite` gains an HTTP delete after the overwrite (as the 009 analysis planned), and a new CLI test checks later reads answer 404.

## R4 Not in scope

Conditional deletes are ignored; multi-object delete (`POST ?delete`) stays 501 from routing (003); no delete markers.
