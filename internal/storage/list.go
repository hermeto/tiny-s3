package storage

import (
	"sort"
	"strings"
	"time"
)

// ListOptions selects a page of a bucket listing.
type ListOptions struct {
	// Prefix limits the listing to keys that start with it.
	Prefix string
	// Delimiter, when set, rolls every key whose rest (after Prefix)
	// contains it into one common prefix: Prefix + rest up to and
	// including the first Delimiter.
	Delimiter string
	// After resumes strictly after this key; with AfterGroup, after every
	// key that starts with it (a common prefix a previous page ended on).
	After      string
	AfterGroup bool
	// MaxKeys is the most entries (objects plus common prefixes) to return;
	// 0 or less returns an empty, complete page.
	MaxKeys int
}

// ObjectSummary is one listed object.
type ObjectSummary struct {
	Key          string
	Size         int64
	MD5          string // lowercase hex
	LastModified time.Time
}

// ListResult is a page of a bucket listing.
type ListResult struct {
	Objects        []ObjectSummary
	CommonPrefixes []string
	// Truncated reports whether more entries follow this page; Next is then
	// its last entry, with NextGroup set when that entry is a common prefix.
	// Passing them back as After and AfterGroup continues the listing.
	Truncated bool
	Next      string
	NextGroup bool
}

// List returns a page of bucket's objects in UTF-8 byte order from the
// in-memory index; it reads no file. Each common prefix is one entry: the
// walk jumps past all of its keys with a binary search.
func (s *Store) List(bucket string, opts ListOptions) ListResult {
	s.index.mu.RLock()
	defer s.index.mu.RUnlock()
	var res ListResult
	b, ok := s.index.buckets[bucket]
	if !ok || opts.MaxKeys <= 0 {
		return res
	}
	keys := b.keys
	i := sort.SearchStrings(keys, opts.Prefix)
	switch {
	case opts.After != "" && opts.AfterGroup:
		i = max(i, pastPrefix(keys, opts.After))
	case opts.After != "":
		i = max(i, sort.Search(len(keys), func(j int) bool { return keys[j] > opts.After }))
	}
	for i < len(keys) {
		key := keys[i]
		if !strings.HasPrefix(key, opts.Prefix) {
			break
		}
		if len(res.Objects)+len(res.CommonPrefixes) == opts.MaxKeys {
			res.Truncated = true
			break
		}
		if cp, ok := commonPrefix(key, opts.Prefix, opts.Delimiter); ok {
			res.CommonPrefixes = append(res.CommonPrefixes, cp)
			res.Next, res.NextGroup = cp, true
			i = pastPrefix(keys, cp)
			continue
		}
		e := b.entries[key]
		res.Objects = append(res.Objects, ObjectSummary{Key: key, Size: e.size, MD5: e.md5, LastModified: e.lastModified})
		res.Next, res.NextGroup = key, false
		i++
	}
	if !res.Truncated {
		res.Next, res.NextGroup = "", false
	}
	return res
}

// commonPrefix returns the common prefix key rolls into, if any.
func commonPrefix(key, prefix, delimiter string) (string, bool) {
	if delimiter == "" {
		return "", false
	}
	rest := key[len(prefix):]
	j := strings.Index(rest, delimiter)
	if j < 0 {
		return "", false
	}
	return prefix + rest[:j+len(delimiter)], true
}

// pastPrefix returns the index of the first key after every key starting
// with p. Keys starting with p are contiguous in sorted order, so the
// predicate below is false up to the end of that run and true after it.
func pastPrefix(keys []string, p string) int {
	return sort.Search(len(keys), func(i int) bool {
		return keys[i] > p && !strings.HasPrefix(keys[i], p)
	})
}
