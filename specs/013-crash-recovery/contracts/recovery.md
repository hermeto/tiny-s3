# Contract: Startup recovery

## Storage

```go
type RecoveryReport struct {
    TmpRemoved, OrphansRemoved, ObjectsIndexed, RecordsSkipped, Buckets int
}
func Open(fsys fsx.FS, root string) (*Store, error) // performs recovery
func (s *Store) Recovery() RecoveryReport              // what Open did
```

`Open` fails, naming the path, on any I/O error during recovery, including a failed removal.

## Log line (stdout, JSON, before `listening`)

```json
{"time":"…","level":"INFO","msg":"recovery","tmp_removed":10,"orphans_removed":1,"objects_indexed":10,"records_skipped":0,"buckets":1}
```

## Guarantees checked end to end

| After | Guarantee |
|-------|-----------|
| SIGKILL during 20 uploads + restart | acknowledged uploads listed and readable (MD5 = ETag = uploaded body); unacknowledged ones absent; `tmp/` empty; listing = scan of `meta/`; `data/` files = listed objects |
| 50 concurrent PUTs to one key | the key holds exactly one of the bodies; one body file; `tmp/` empty |
