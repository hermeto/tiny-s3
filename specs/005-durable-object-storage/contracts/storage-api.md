# Contract: `internal/storage` Go API

```go
package storage

var (
    ErrNoSuchKey       // key has no committed record
    ErrIncompleteBody  // body length differs from declared size
    ErrBadDigest       // body MD5 differs from supplied digest
    ErrEntityTooLarge  // declared size > 5 GiB
    ErrKeyTooLong      // key > 1024 bytes
    ErrInvalidArgument // empty or non-UTF-8 key, negative size
    ErrCorruptRecord   // metadata record cannot be decoded (Head/Get); Put replaces and Delete removes it
)

const MaxObjectSize = 5 << 30
const MaxKeyLength  = 1024

type Info struct {
    Bucket, Key  string
    Size         int64
    MD5          string    // lowercase hex
    LastModified time.Time // UTC
}

// Reader is an open, immutable object body. The caller must Close it.
type Reader interface { io.Reader; io.ReaderAt; io.Seeker; io.Closer }

// Open creates tmp/, data/ and meta/ under root and removes leftovers in tmp/.
func Open(fsys fsx.FS, root string) (*Store, error)

// Put streams exactly size bytes from body. contentMD5 is the raw 16-byte
// digest, or nil when the client sent none.
func (s *Store) Put(ctx context.Context, bucket, key string, body io.Reader, size int64, contentMD5 []byte) (Info, error)

func (s *Store) Get(bucket, key string) (Info, Reader, error)
func (s *Store) Head(bucket, key string) (Info, error)
func (s *Store) Delete(bucket, key string) error // nil when the key does not exist
```

Errors are matched with `errors.Is`; I/O errors are wrapped with the operation and path. The HTTP mapping (IncompleteBody 400, BadDigest 400, EntityTooLarge 400, KeyTooLongError 400, NoSuchKey 404, InvalidArgument 400, everything else InternalError 500) belongs to the S3 layer.
