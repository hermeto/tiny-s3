# Data Model: Crash Recovery and Concurrency Guarantees

No on-disk format change.

## RecoveryReport (storage)

| Field | Meaning |
|-------|---------|
| TmpRemoved | Entries removed from `tmp/` |
| OrphansRemoved | Entries removed from `data/` because no valid record names them |
| ObjectsIndexed | Records indexed |
| RecordsSkipped | Records left out: undecodable, misplaced (file name ≠ key hash), or body missing |
| Buckets | Buckets loaded |

## Invariants after `Open`

- `tmp/` is empty.
- Every entry of `data/` is the body of exactly one indexed record.
- The index equals the set of valid records whose body exists.
