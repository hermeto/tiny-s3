// Package storage is the tiny-s3 storage engine: it stores, reads and deletes
// objects on local disk, independent of HTTP.
//
// Layout under the store root:
//
//	tmp/              files of in-flight Puts; never visible as objects
//	data/<version>    one immutable body per committed Put
//	meta/<hash>.json  the metadata record of a key; its presence is the
//	                  commit point, so a key without it does not exist
//	buckets/<name>.json  the record of a bucket; its presence is the commit
//	                  point, and every record is loaded into memory by Open
//
// <hash> is the hex SHA-256 of len(bucket) ":" bucket key. The length prefix
// makes the mapping unambiguous whatever the bucket contains, and the fixed
// hex name means no key can address a path outside meta/.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

const (
	tmpDir     = "tmp"
	dataDir    = "data"
	metaDir    = "meta"
	bucketsDir = "buckets"
	dirPerm    = 0o750
)

// Errors returned by the engine. The S3 layer maps them to S3 error codes.
var (
	ErrNoSuchKey       = errors.New("storage: no such key")
	ErrIncompleteBody  = errors.New("storage: body length differs from declared length")
	ErrBadDigest       = errors.New("storage: body MD5 differs from Content-MD5")
	ErrEntityTooLarge  = errors.New("storage: object larger than the maximum allowed size")
	ErrKeyTooLong      = errors.New("storage: key longer than 1024 bytes")
	ErrInvalidArgument = errors.New("storage: invalid argument")
	ErrCorruptRecord   = errors.New("storage: corrupt metadata record")
)

const (
	// MaxObjectSize is the S3 single-PUT limit, 5 GiB.
	MaxObjectSize = 5 << 30
	// MaxKeyLength is the S3 key limit in bytes.
	MaxKeyLength = 1024
)

// DefaultContentType is the content type of an object stored without one,
// as in S3.
const DefaultContentType = "binary/octet-stream"

// Info is the metadata of a committed object.
type Info struct {
	Bucket       string
	Key          string
	Size         int64
	MD5          string    // lowercase hex
	LastModified time.Time // UTC
	ContentType  string
	Metadata     map[string]string // user metadata; nil when there is none
}

// PutOptions are the optional inputs of Put.
type PutOptions struct {
	// ContentMD5 is the raw 16-byte digest the client sent, or nil.
	ContentMD5 []byte
	// ContentType is stored as is; empty stores DefaultContentType.
	ContentType string
	// Metadata is the user metadata to store; it replaces any previous one.
	Metadata map[string]string
	// Verify, when set, is called once the whole body is read and its MD5
	// checked, before anything is synced or committed. An error fails the
	// Put; it is returned wrapped.
	Verify func() error
}

// Reader is an open, immutable object body. It keeps yielding the version it
// was opened on even if the key is overwritten or deleted. Callers must Close
// it.
type Reader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

// Store is an object store rooted at one directory. Only one process may use
// a root at a time; a Store is safe for concurrent use by many goroutines.
type Store struct {
	fsys  fsx.FS
	root  string
	now   func() time.Time
	locks keyLocks

	// bmu guards buckets. CreateBucket holds it exclusively for its whole
	// write path, so concurrent creates of one name have one winner.
	bmu     sync.RWMutex
	buckets map[string]Bucket
}

// Open creates tmp/, data/, meta/ and buckets/ under root if needed, removes
// every leftover in tmp/ from writes interrupted by a crash, and loads every
// bucket record. A bucket record that cannot be read or is invalid fails Open.
func Open(fsys fsx.FS, root string) (*Store, error) {
	for _, dir := range []string{tmpDir, dataDir, metaDir, bucketsDir} {
		if err := fsys.MkdirAll(filepath.Join(root, dir), dirPerm); err != nil {
			return nil, fmt.Errorf("storage: create %s: %w", dir, err)
		}
	}
	tmp := filepath.Join(root, tmpDir)
	leftovers, err := fsys.ReadDir(tmp)
	if err != nil {
		return nil, fmt.Errorf("storage: list %s: %w", tmp, err)
	}
	for _, de := range leftovers {
		if err := fsys.Remove(filepath.Join(tmp, de.Name())); err != nil {
			return nil, fmt.Errorf("storage: remove leftover: %w", err)
		}
	}
	s := &Store{fsys: fsys, root: root, now: time.Now}
	buckets, err := s.loadBuckets()
	if err != nil {
		return nil, err
	}
	s.buckets = buckets
	return s, nil
}

func validateKey(key string) error {
	switch {
	case len(key) > MaxKeyLength:
		return ErrKeyTooLong
	case key == "":
		return fmt.Errorf("%w: empty key", ErrInvalidArgument)
	case !utf8.ValidString(key):
		return fmt.Errorf("%w: key is not valid UTF-8", ErrInvalidArgument)
	}
	return nil
}

func validateSize(size int64) error {
	switch {
	case size < 0:
		return fmt.Errorf("%w: negative length %d", ErrInvalidArgument, size)
	case size > MaxObjectSize:
		return ErrEntityTooLarge
	}
	return nil
}

// recordName is the file name, without extension, of a key's metadata record.
func recordName(bucket, key string) string {
	sum := sha256.Sum256([]byte(strconv.Itoa(len(bucket)) + ":" + bucket + key))
	return hex.EncodeToString(sum[:])
}

func (s *Store) recordPath(name string) string {
	return filepath.Join(s.root, metaDir, name+".json")
}

func (s *Store) dataPath(version string) string {
	return filepath.Join(s.root, dataDir, version)
}
