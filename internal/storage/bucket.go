package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Errors returned by the bucket operations.
var (
	ErrInvalidBucketName = errors.New("storage: invalid bucket name")
	ErrNoSuchBucket      = errors.New("storage: no such bucket")
	ErrBucketExists      = errors.New("storage: bucket already exists")
)

// Bucket is a committed bucket.
type Bucket struct {
	Name         string
	CreationDate time.Time // UTC
}

var (
	reservedBucketPrefixes = []string{"xn--", "sthree-", "amzn-s3-demo-"}
	reservedBucketSuffixes = []string{"-s3alias", "--ol-s3", ".mrap", "--x-s3", "--table-s3"}
)

// ValidBucketName reports whether name follows the S3 rules for general
// purpose buckets: 3 to 63 characters from [a-z0-9.-], starting and ending
// with a letter or digit, no two adjacent dots, not an IPv4 address, and none
// of the reserved prefixes and suffixes. A valid name is also a safe file
// name: it never contains "/" and is never "." or "..".
func ValidBucketName(name string) bool {
	if len(name) < 3 || len(name) > 63 {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !isLowerAlnum(c) && c != '.' && c != '-' {
			return false
		}
	}
	if !isLowerAlnum(name[0]) || !isLowerAlnum(name[len(name)-1]) || strings.Contains(name, "..") {
		return false
	}
	for _, p := range reservedBucketPrefixes {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	for _, s := range reservedBucketSuffixes {
		if strings.HasSuffix(name, s) {
			return false
		}
	}
	return !looksLikeIPv4(name)
}

func isLowerAlnum(c byte) bool {
	return 'a' <= c && c <= 'z' || '0' <= c && c <= '9'
}

// looksLikeIPv4 reports whether name is four dot-separated groups of digits.
// The name is already known to hold only [a-z0-9.-] without empty groups.
func looksLikeIPv4(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		for i := range len(p) {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

const bucketRecordVersion = 1

// bucketRecord is the on-disk record of a bucket (buckets/<name>.json).
type bucketRecord struct {
	V       int       `json:"v"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

func (s *Store) bucketPath(name string) string {
	return filepath.Join(s.root, bucketsDir, name+".json")
}

// CreateBucket durably creates the bucket name and returns it. The record is
// written to tmp/, synced, renamed into buckets/ and buckets/ is synced before
// the bucket becomes visible. It fails with ErrInvalidBucketName before any
// disk access, and with ErrBucketExists when the bucket already exists.
func (s *Store) CreateBucket(name string) (Bucket, error) {
	if !ValidBucketName(name) {
		return Bucket{}, ErrInvalidBucketName
	}
	s.bmu.Lock()
	defer s.bmu.Unlock()
	if _, ok := s.buckets[name]; ok {
		return Bucket{}, ErrBucketExists
	}

	b := Bucket{Name: name, CreationDate: s.now().UTC()}
	raw, err := json.Marshal(bucketRecord{V: bucketRecordVersion, Name: name, Created: b.CreationDate})
	if err != nil {
		return Bucket{}, fmt.Errorf("storage: encode bucket: %w", err)
	}
	tmp, err := s.writeTemp("bucket-*", raw)
	if err != nil {
		return Bucket{}, err
	}
	path := s.bucketPath(name)
	if err := s.fsys.Rename(tmp, path); err != nil {
		_ = s.fsys.Remove(tmp)
		return Bucket{}, fmt.Errorf("storage: rename bucket record: %w", err)
	}
	if err := s.syncDir(bucketsDir); err != nil {
		if rbErr := s.removeBucketRecord(path); rbErr != nil {
			// The record may be durable; it is complete, so keep the
			// bucket rather than hide one that can reappear.
			s.buckets[name] = b
			return Bucket{}, errors.Join(err, rbErr)
		}
		return Bucket{}, err
	}
	s.buckets[name] = b
	return b, nil
}

// HeadBucket returns the bucket name, ErrNoSuchBucket when it does not exist,
// or ErrInvalidBucketName when name is not a valid bucket name.
func (s *Store) HeadBucket(name string) (Bucket, error) {
	if !ValidBucketName(name) {
		return Bucket{}, ErrInvalidBucketName
	}
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	b, ok := s.buckets[name]
	if !ok {
		return Bucket{}, ErrNoSuchBucket
	}
	return b, nil
}

// ListBuckets returns every bucket sorted by name, as a new slice.
func (s *Store) ListBuckets() []Bucket {
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	out := make([]Bucket, 0, len(s.buckets))
	for _, b := range s.buckets {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b Bucket) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// removeBucketRecord rolls back a bucket record whose directory sync failed.
func (s *Store) removeBucketRecord(path string) error {
	if err := s.fsys.Remove(path); err != nil {
		return fmt.Errorf("storage: rollback: remove bucket record: %w", err)
	}
	return s.syncDir(bucketsDir)
}

// loadBuckets reads every record in buckets/.
func (s *Store) loadBuckets() (map[string]Bucket, error) {
	dir := filepath.Join(s.root, bucketsDir)
	des, err := s.fsys.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: list %s: %w", dir, err)
	}
	buckets := make(map[string]Bucket, len(des))
	for _, de := range des {
		b, err := s.readBucketRecord(filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, err
		}
		buckets[b.Name] = b
	}
	return buckets, nil
}

// readBucketRecord decodes and checks the bucket record at path. I/O errors
// are returned wrapped; anything else wrong with the entry is
// ErrCorruptRecord. Both name the path.
func (s *Store) readBucketRecord(path string) (Bucket, error) {
	name, ok := strings.CutSuffix(filepath.Base(path), ".json")
	if !ok || !ValidBucketName(name) {
		return Bucket{}, fmt.Errorf("%w: %s is not a bucket record name", ErrCorruptRecord, path)
	}
	f, err := s.fsys.Open(path)
	if err != nil {
		return Bucket{}, fmt.Errorf("storage: open bucket record %s: %w", path, err)
	}
	// A close error on a read-only file cannot lose data.
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxRecordSize+1))
	if err != nil {
		return Bucket{}, fmt.Errorf("storage: read bucket record %s: %w", path, err)
	}
	if len(raw) > maxRecordSize {
		return Bucket{}, fmt.Errorf("%w: %s is over %d bytes", ErrCorruptRecord, path, maxRecordSize)
	}
	var rec bucketRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Bucket{}, fmt.Errorf("%w: %s: %v", ErrCorruptRecord, path, err)
	}
	switch {
	case rec.V != bucketRecordVersion:
		return Bucket{}, fmt.Errorf("%w: %s has version %d", ErrCorruptRecord, path, rec.V)
	case rec.Name != name:
		return Bucket{}, fmt.Errorf("%w: %s names bucket %q", ErrCorruptRecord, path, rec.Name)
	case rec.Created.IsZero():
		return Bucket{}, fmt.Errorf("%w: %s has no creation date", ErrCorruptRecord, path)
	}
	return Bucket{Name: name, CreationDate: rec.Created.UTC()}, nil
}
