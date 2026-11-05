# Contract: storage bucket API

Package `internal/storage`. All methods are safe for concurrent use.

```go
var (
    ErrInvalidBucketName = errors.New("storage: invalid bucket name")
    ErrNoSuchBucket      = errors.New("storage: no such bucket")
    ErrBucketExists      = errors.New("storage: bucket already exists")
)

type Bucket struct {
    Name         string
    CreationDate time.Time // UTC
}

func ValidBucketName(name string) bool

func (s *Store) CreateBucket(name string) (Bucket, error)
func (s *Store) HeadBucket(name string) (Bucket, error)
func (s *Store) ListBuckets() []Bucket
```

| Call | Result |
|------|--------|
| `Open` | Also creates `buckets/` and loads every record; a bad record fails with `ErrCorruptRecord`, an I/O error with that error, both naming the path. |
| `CreateBucket(invalid)` | `ErrInvalidBucketName`, no disk access |
| `CreateBucket(existing)` | `ErrBucketExists`, nothing written |
| `CreateBucket(new)` | The new `Bucket`, returned only after the record is renamed into `buckets/` and `buckets/` is synced |
| `CreateBucket` I/O failure | Wrapped error; temp file removed; bucket absent (unless rollback also failed: then present and complete, both errors joined) |
| `HeadBucket(invalid)` | `ErrInvalidBucketName` |
| `HeadBucket(missing)` | `ErrNoSuchBucket` |
| `HeadBucket(existing)` | The `Bucket` |
| `ListBuckets()` | Every bucket, sorted by name ascending; empty slice when none |

Operation order of a successful create, as recorded by `fsx.Faulty`:
`create tmp/bucket-*` → `write` → `sync` → `close` → `rename → buckets/<name>.json` → `opendir buckets` → `dirsync` → `close`.
