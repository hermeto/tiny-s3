package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"
)

const (
	recordVersion = 1
	// maxRecordSize bounds a record read; a valid record with a 1024-byte
	// key is far smaller.
	maxRecordSize = 64 << 10
)

// record is the on-disk metadata record of a key (meta/<name>.json).
type record struct {
	V            int       `json:"v"`
	Bucket       string    `json:"bucket"`
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	MD5          string    `json:"md5"`
	LastModified time.Time `json:"last_modified"`
	Data         string    `json:"data"`
}

func (r record) info() Info {
	return Info{Bucket: r.Bucket, Key: r.Key, Size: r.Size, MD5: r.MD5, LastModified: r.LastModified}
}

// readRecord returns the committed record of name, ErrNoSuchKey when there is
// none, or ErrCorruptRecord when it cannot be decoded.
func (s *Store) readRecord(name string) (record, error) {
	path := s.recordPath(name)
	f, err := s.fsys.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return record{}, ErrNoSuchKey
	}
	if err != nil {
		return record{}, fmt.Errorf("storage: open record: %w", err)
	}
	// A close error on a read-only file cannot lose data.
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxRecordSize+1))
	if err != nil {
		return record{}, fmt.Errorf("storage: read record %s: %w", path, err)
	}
	if len(b) > maxRecordSize {
		return record{}, fmt.Errorf("%w: %s is over %d bytes", ErrCorruptRecord, path, maxRecordSize)
	}
	var rec record
	if err := json.Unmarshal(b, &rec); err != nil {
		return record{}, fmt.Errorf("%w: %s: %v", ErrCorruptRecord, path, err)
	}
	if rec.V != recordVersion {
		return record{}, fmt.Errorf("%w: %s has version %d", ErrCorruptRecord, path, rec.V)
	}
	return rec, nil
}

// Head returns the metadata of a committed object. It takes the key lock: a
// record is always read whole thanks to the atomic rename, but without the
// lock Head could see a record between the commit rename and the meta/ sync,
// before it is durable or just before a rollback removes it.
func (s *Store) Head(bucket, key string) (Info, error) {
	if err := validateKey(key); err != nil {
		return Info{}, err
	}
	name := recordName(bucket, key)
	unlock := s.locks.lock(name)
	defer unlock()
	rec, err := s.readRecord(name)
	if err != nil {
		return Info{}, err
	}
	return rec.info(), nil
}

// Get returns the metadata of a committed object and an open reader over its
// body. The record read and the data open happen under the key lock, so a
// concurrent commit or Delete cannot remove the data file in between; once
// open, the reader is unaffected by later overwrites and deletes.
func (s *Store) Get(bucket, key string) (Info, Reader, error) {
	if err := validateKey(key); err != nil {
		return Info{}, nil, err
	}
	name := recordName(bucket, key)
	unlock := s.locks.lock(name)
	defer unlock()
	rec, err := s.readRecord(name)
	if err != nil {
		return Info{}, nil, err
	}
	f, err := s.fsys.Open(s.dataPath(rec.Data))
	if err != nil {
		return Info{}, nil, fmt.Errorf("storage: open data of %q: %w", key, err)
	}
	return rec.info(), f, nil
}
