# Contract: `internal/fsx` read-side additions

```go
// ReadFile is a file opened read-only.
type ReadFile interface {
    Name() string
    io.Reader
    io.ReaderAt
    io.Seeker
    io.Closer
}

type FS interface {
    // existing: MkdirAll, CreateTemp, OpenDir, Rename, Remove
    Open(path string) (ReadFile, error)          // os.Open
    ReadDir(path string) ([]fs.DirEntry, error)  // os.ReadDir
}

const (
    OpOpen    = "open"    // FS.Open, matched on path
    OpRead    = "read"    // ReadFile.Read and ReadAt, matched on file name
    OpReadDir = "readdir" // FS.ReadDir, matched on path
)
```

- `OS.Open` and `OS.ReadDir` return the `os` errors unchanged (`errors.Is(err, fs.ErrNotExist)` holds).
- `Faulty.Open` records `OpOpen` and wraps the returned file; its `Read`/`ReadAt` record `OpRead` with `Requested`/`Written`, `Close` records `OpClose`, `Seek` is not recorded.
- An injected read fault returns `(0, Err)`.
