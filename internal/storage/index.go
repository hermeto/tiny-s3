package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// entry is what the index keeps per object: the fields a listing returns.
type entry struct {
	size         int64
	md5          string
	lastModified time.Time
}

func entryOf(info Info) entry {
	return entry{size: info.Size, md5: info.MD5, lastModified: info.LastModified}
}

// bucketIndex holds one bucket's keys in UTF-8 byte order and their entries.
type bucketIndex struct {
	keys    []string
	entries map[string]entry
}

// objectIndex is the in-memory ordered index of every committed object. It
// mirrors the metadata records: it is rebuilt from them by Open and changed,
// under the key lock, exactly when a record file is renamed in, rolled back
// or removed, so a listing never disagrees with Head.
//
// Keys are a sorted slice per bucket: lookups and page starts are binary
// searches, and inserting a new key shifts the slice, which is cheap at the
// scale this project targets (see the README).
type objectIndex struct {
	mu      sync.RWMutex
	buckets map[string]*bucketIndex
}

func newObjectIndex() *objectIndex {
	return &objectIndex{buckets: make(map[string]*bucketIndex)}
}

// put sets the entry of bucket/key, inserting the key in order if it is new.
func (x *objectIndex) put(bucket, key string, e entry) {
	x.mu.Lock()
	defer x.mu.Unlock()
	b, ok := x.buckets[bucket]
	if !ok {
		b = &bucketIndex{entries: make(map[string]entry)}
		x.buckets[bucket] = b
	}
	if _, exists := b.entries[key]; !exists {
		i, _ := slices.BinarySearch(b.keys, key)
		b.keys = slices.Insert(b.keys, i, key)
	}
	b.entries[key] = e
}

// remove drops bucket/key if present; a bucket left empty is dropped too.
func (x *objectIndex) remove(bucket, key string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	b, ok := x.buckets[bucket]
	if !ok {
		return
	}
	if _, exists := b.entries[key]; !exists {
		return
	}
	delete(b.entries, key)
	i, _ := slices.BinarySearch(b.keys, key)
	b.keys = slices.Delete(b.keys, i, i+1)
	if len(b.keys) == 0 {
		delete(x.buckets, bucket)
	}
}

// loadIndex rebuilds the index from meta/ and returns the bodies the indexed
// records name. bodies is the set of files in data/. A record that cannot be
// decoded, sits under a file name that is not its key's hash, or names a body
// that is missing is skipped and counted: Head and Get keep reporting it and
// Put and Delete repair it, as for any corrupt record. Entries that are not
// records are ignored. I/O errors fail.
func (s *Store) loadIndex(bodies map[string]bool) (map[string]bool, error) {
	dir := filepath.Join(s.root, metaDir)
	des, err := s.fsys.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: list %s: %w", dir, err)
	}
	referenced := make(map[string]bool, len(des))
	for _, de := range des {
		name, ok := strings.CutSuffix(de.Name(), ".json")
		if !ok {
			continue
		}
		rec, err := s.readRecord(name)
		switch {
		case errors.Is(err, ErrCorruptRecord), errors.Is(err, ErrNoSuchKey):
			// ErrNoSuchKey: an entry that cannot be opened as a file,
			// such as a dangling link.
			s.recovery.RecordsSkipped++
			continue
		case err != nil:
			return nil, err
		case recordName(rec.Bucket, rec.Key) != name, !bodies[rec.Data]:
			s.recovery.RecordsSkipped++
			continue
		}
		s.index.put(rec.Bucket, rec.Key, entryOf(rec.info()))
		referenced[rec.Data] = true
		s.recovery.ObjectsIndexed++
	}
	return referenced, nil
}
