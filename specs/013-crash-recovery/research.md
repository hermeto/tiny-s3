# Research: Crash Recovery and Concurrency Guarantees

Every decision took the recommended option, as authorized by the feature owner.

## R1 What a crash can leave behind

The 005 write order is: body temp file in `tmp/` → sync → rename to `data/<version>` → sync `data/` → record temp file in `tmp/` → sync → rename over `meta/<hash>.json` (commit point) → sync `meta/` → remove the previous body. So a crash can leave: temp files in `tmp/` (any step before a rename), a body in `data/` with no record (between the body rename and the record rename), and a previous version's body whose record was replaced (between the commit and its removal; also a failed best-effort removal or a failed delete's data removal). It can never leave a record that points at a missing or partial body, because the body is renamed and synced first.

## R2 Startup sequence

- **Decision**: `storage.Open` does, in order: create the layout; remove every entry in `tmp/` (counted); load buckets (006); list `data/` into a set; read every `meta/` record, skipping (and counting) undecodable, misplaced (012) and body-less records, indexing the rest and marking their bodies referenced; remove every `data/` entry not referenced (counted). It stores the counts as a `RecoveryReport{TmpRemoved, OrphansRemoved, ObjectsIndexed, RecordsSkipped, Buckets}` returned by `Store.Recovery()`.
- **Rationale**: Orphans can only be identified after every record is read; listing `data/` once gives both the existence check for records and the candidates for removal without extra file opens. One process owns the directory, so nothing races with the cleanup.
- **Errors**: any I/O failure, including a failed removal, fails `Open` with an error naming the path (FR-007); `s3d` exits 1.
- **No directory sync for removals** (clarification 3): removing an unreferenced body or a temp file is idempotent; if a crash undoes it, the next startup repeats it.

## R3 Logging

- **Decision**: `app.run` logs `{"msg":"recovery","tmp_removed":…,"orphans_removed":…,"objects_indexed":…,"records_skipped":…,"buckets":…}` right after `storage.Open` and before `listening`.

## R4 Proving it end to end

- **Kill during uploads**: the e2e test starts 20 uploads of distinct 4 MiB bodies through `io.Pipe`. Ten send everything and are acknowledged; ten send 1 MiB and stall. The test polls the data directory until `tmp/` holds the ten in-flight temp files (so the kill provably lands mid-upload), plants one unreferenced body in `data/`, and sends SIGKILL. After a restart it requires: the `recovery` line reports 10 temp files and 1 orphan removed and 10 objects indexed; `tmp/` is empty; `aws s3api list-objects-v2` lists exactly the ten acknowledged keys; `get-object` of each returns bytes whose MD5 equals the ETag and the uploaded body; the listing equals a scan of the `meta/` records on disk; `data/` holds exactly ten files.
- **50 concurrent PUTs to one key**: 50 HTTP clients upload distinct bodies to one key at once; afterwards `get-object` returns exactly one of the bodies with a matching ETag, `data/` holds one file and `tmp/` is empty.
- **Randomized engine test**: many rounds of random Put/overwrite/Delete on a few keys with a random fault injected per operation, then reopen; the index must equal an independent scan of the records whose bodies exist, and `data/` must hold exactly the referenced bodies.

## R5 Limits

- SIGKILL keeps the page cache, so it proves ordering on the filesystem, not power-loss durability; the 005 operation-order tests cover the fsync calls (README states this).
- Bodies are not checksummed at startup (would read all data); they are complete by construction.
