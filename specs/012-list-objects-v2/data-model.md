# Data Model: Ordered Index and ListObjectsV2

## Index (memory only)

| Level | Content |
|-------|---------|
| store | `map[bucket]*bucketIndex`, one `RWMutex` |
| bucketIndex | `keys []string` (sorted, UTF-8 byte order) and `entries map[string]entry` |
| entry | `size int64`, `md5 string`, `lastModified time.Time` |

Rebuilt by `Open` from `meta/*.json` (005/007 records carry `bucket` and `key`). Changed only under the key lock, at the moment a record file is renamed in, rolled back or removed.

## ListOptions / ListResult (storage)

| Field | Meaning |
|-------|---------|
| Prefix, Delimiter | filter and grouping |
| After, AfterGroup | resume strictly after a key, or after every key starting with After |
| MaxKeys | entries per page (keys + common prefixes), 0 → empty page |
| Objects | `[]ObjectSummary{Key, Size, MD5, LastModified}` |
| CommonPrefixes | `[]string` |
| Truncated, Next, NextGroup | whether more entries exist, and the last entry returned |
