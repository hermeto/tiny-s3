# Research: Ordered Index and ListObjectsV2

Every decision took the recommended option, as authorized by the feature owner.

## R1 Index structure

- **Decision**: `internal/storage/index.go`: one `objectIndex` per store, guarded by its own `sync.RWMutex`, holding per bucket a sorted `[]string` of keys and a `map[string]entry` (`size`, `md5`, `lastModified`). Insert and delete use `slices.BinarySearch` plus `slices.Insert`/`slices.Delete`.
- **Rationale**: Boring, small, easy to prove correct and to cover 100%. Lookups and page starts are O(log n); a page is a contiguous slice walk. Inserting a new key shifts the slice (O(n) memmove), which costs microseconds at 10k keys and about a millisecond at 1M keys per bucket.
- **Alternatives considered**: A B-tree or skip list (O(log n) inserts; more code for a scale this project does not target; listed as a next step in the README). Scanning `meta/` per request (violates FR-007 and the constitution's in-memory index).

## R2 Keeping the index equal to the records on disk

- **Decision**: The index mirrors the record files, under the key lock, at the point each file changes: Put's commit sets the entry right after the record rename (before the `meta/` sync), its rollback restores the previous entry or removes it, and Delete removes the entry right after the record removal. So a listing never shows a key `Head` would not find, or the other way round.
- **Rationale**: Acknowledged writes and deletes are always reflected (FR-003); a failed one leaves the index matching what `Head` reports.

## R3 Rebuild at startup

- **Decision**: `Open` reads every `meta/*.json` record and indexes it. An undecodable record (`ErrCorruptRecord`) is skipped; an I/O error fails `Open`. Orphan cleanup, counts and logging are feature 013.

## R4 Listing algorithm

- **Decision**: `Store.List(bucket, ListOptions{Prefix, Delimiter, After, AfterGroup, MaxKeys})` binary-searches the first key greater than `After` (or, when `AfterGroup`, greater than every key starting with `After`) and not below `Prefix`, then walks keys while they start with `Prefix`. With a delimiter, a key whose rest (after the prefix) contains it rolls into the common prefix `Prefix + rest[:i+len(delim)]`, emitted once; the walk then jumps past every key of that group with another binary search. It stops after `MaxKeys` entries (keys plus prefixes); `Truncated` is whether another entry exists, and `Next` is the last entry (with `NextGroup` when it is a common prefix).
- **Continuation token**: `base64.RawURLEncoding` of `"k" + key` or `"g" + commonPrefix`; anything else is `InvalidArgument`. Tokens encode positions, not memory, so they survive restarts.
- **Consistency**: Every key that exists for the whole listing is after exactly one page's end and before the next, so it appears once whatever is written meanwhile (SC-001).

## R5 HTTP

- **Decision**: `ListObjectsV2Op` parses `prefix`, `delimiter`, `start-after`, `continuation-token`, `max-keys` (default and cap 1000, invalid → `InvalidArgument`), `encoding-type` (only `url`, else `InvalidArgument`) and answers `ListBucketResult` in the S3 namespace: `Name`, `Prefix`, `Delimiter`, `MaxKeys`, `EncodingType`, `KeyCount`, `IsTruncated`, `ContinuationToken`, `NextContinuationToken`, `StartAfter`, `Contents` (`Key`, `LastModified` as `2006-01-02T15:04:05.000Z`, quoted `ETag`, `Size`, `StorageClass` `STANDARD`) and `CommonPrefixes`.
- **URL encoding**: every byte except unreserved characters (`A-Z a-z 0-9 - . _ ~`) and `/` becomes `%XX`, so a space is `%20` and `+` is `%2B`, correct under both `unquote` and `unquote_plus` (the packaged botocore could not be inspected; the e2e test lists keys with spaces and `+` through the CLI).
