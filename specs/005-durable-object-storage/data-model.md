# Data Model: Durable Object Storage

## Info (returned by Put, Head, Get)

| Field | Type | Rule |
|-------|------|------|
| Bucket | string | Namespace component; not validated by the engine |
| Key | string | Non-empty, valid UTF-8, ≤ 1024 bytes |
| Size | int64 | 0 ≤ Size ≤ 5 GiB |
| MD5 | string | 32 lowercase hex characters of the body MD5 |
| LastModified | time.Time | UTC, time of commit |

## Metadata record (`meta/<hex sha256(len(bucket) ":" bucket key)>.json`)

```json
{"v":1,"bucket":"b","key":"k","size":5,"md5":"5d41402abc4b2a76b9719d911017c592","last_modified":"2026-11-04T12:00:00.123456789Z","data":"<version>"}
```

- Presence of the record defines existence (commit point).
- `v` must be 1; anything else, invalid JSON or a record over 64 KiB is `ErrCorruptRecord`.
- `data` names the immutable data version file.

## Data version (`data/<version>`)

- Exactly `size` bytes of body. Immutable once renamed in. Removed by the next commit or Delete on the same key, after the record change is durable.

## Temporary file (`tmp/put-*`, `tmp/meta-*`)

- Exists only while a Put runs; removed on every failure; all entries removed by `Open`.

## State transitions per key

```text
absent --Put commit--> present(v1) --Put commit--> present(v2) --Delete--> absent
absent --Delete--> absent                     (idempotent)
any    --failed Put--> unchanged              (temp and new data removed; rollback if needed)
```
