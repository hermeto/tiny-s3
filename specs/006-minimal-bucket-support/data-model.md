# Data Model: Minimal Bucket Support

## Bucket

| Field | Type | Rules |
|-------|------|-------|
| Name | string | Valid S3 general purpose bucket name (research R5); unique in the store. |
| CreationDate | time (UTC) | Set once from the store clock at create; never changes. |

State: a bucket either does not exist or exists. There is no transition back (DeleteBucket is out of scope).

```text
(absent) --CreateBucket ok--> (exists)
(exists) --CreateBucket--> ErrBucketExists, unchanged
```

## Bucket record (on disk)

Path: `<root>/buckets/<name>.json`. Written through `<root>/tmp/bucket-*` and renamed into place (research R2); its presence after the `buckets/` sync is the commit point.

```json
{"v":1,"name":"demo","created":"2026-11-04T14:30:15.123456789Z"}
```

| Key | Meaning | Load-time check (research R4) |
|-----|---------|-------------------------------|
| `v` | Format version | Must be 1 |
| `name` | Bucket name | Must equal the file name without `.json`, and be valid |
| `created` | Creation date, RFC 3339 with nanoseconds, UTC | Must be non-zero |

A record over 64 KiB or failing a check makes `Open` fail with `ErrCorruptRecord`; an I/O error reading it makes `Open` fail with that error. Both name the path.

## Store layout after this feature

```text
<root>/
├── tmp/       in-flight files (put-*, meta-*, bucket-*); cleared by Open
├── data/      object bodies (005)
├── meta/      object records (005)
└── buckets/   one record per bucket (this feature)
```
