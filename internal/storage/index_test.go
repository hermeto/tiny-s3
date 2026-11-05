package storage

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

// Test helpers: read access to the index without the listing code.

// get returns the entry of bucket/key.
func (x *objectIndex) get(bucket, key string) (entry, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	b, ok := x.buckets[bucket]
	if !ok {
		return entry{}, false
	}
	e, ok := b.entries[key]
	return e, ok
}

// keys returns a copy of bucket's keys in order.
func (x *objectIndex) keys(bucket string) []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	b, ok := x.buckets[bucket]
	if !ok {
		return nil
	}
	return slices.Clone(b.keys)
}

func TestObjectIndexOrderAndReplace(t *testing.T) {
	t.Parallel()
	x := newObjectIndex()
	for _, k := range []string{"b", "a/b", "a", "é", "a/", "Z", "a/b/c", "~", "a/b"} {
		x.put("demo", k, entry{size: int64(len(k))})
	}
	want := []string{"Z", "a", "a/", "a/b", "a/b/c", "b", "~", "é"} // UTF-8 byte order
	if got := x.keys("demo"); !slices.Equal(got, want) {
		t.Fatalf("keys = %q, want %q", got, want)
	}
	x.put("demo", "a", entry{size: 42, md5: "m", lastModified: fixedTime})
	if e, ok := x.get("demo", "a"); !ok || e.size != 42 || e.md5 != "m" || !e.lastModified.Equal(fixedTime) {
		t.Fatalf("get(a) = %+v, %v", e, ok)
	}
	if got := x.keys("demo"); len(got) != len(want) {
		t.Fatalf("replace duplicated a key: %q", got)
	}
}

func TestObjectIndexRemoveAndBuckets(t *testing.T) {
	t.Parallel()
	x := newObjectIndex()
	x.put("one", "k", entry{})
	x.put("two", "k", entry{})
	x.put("two", "j", entry{})
	x.remove("two", "k")
	x.remove("two", "missing")
	x.remove("missing", "k")
	if got := x.keys("one"); !slices.Equal(got, []string{"k"}) {
		t.Fatalf("one = %q", got)
	}
	if got := x.keys("two"); !slices.Equal(got, []string{"j"}) {
		t.Fatalf("two = %q", got)
	}
	x.remove("two", "j")
	if _, ok := x.buckets["two"]; ok {
		t.Fatal("empty bucket kept in the index")
	}
	if got := x.keys("two"); len(got) != 0 {
		t.Fatalf("two after removing everything = %q", got)
	}
	if _, ok := x.get("two", "j"); ok {
		t.Fatal("get found a removed key")
	}
}

func TestEntryFromInfo(t *testing.T) {
	t.Parallel()
	e := entryOf(Info{Size: 3, MD5: "abc", LastModified: time.Unix(5, 0)})
	if e.size != 3 || e.md5 != "abc" || !e.lastModified.Equal(time.Unix(5, 0)) {
		t.Fatalf("entryOf = %+v", e)
	}
}

// indexMatchesHead fails the test unless the index and Head agree on
// bucket/key: both missing, or the same size, MD5 and last-modified.
func indexMatchesHead(t *testing.T, s *Store, bucket, key string) {
	t.Helper()
	e, inIndex := s.index.get(bucket, key)
	info, err := s.Head(bucket, key)
	switch {
	case errors.Is(err, ErrNoSuchKey):
		if inIndex {
			t.Fatalf("%s/%s: in the index (%+v) but Head reports no such key", bucket, key, e)
		}
	case err != nil:
		t.Fatalf("Head(%s/%s): %v", bucket, key, err)
	case !inIndex || e != entryOf(info):
		t.Fatalf("%s/%s: index %+v (present %v), Head %+v", bucket, key, e, inIndex, info)
	}
}

func TestIndexFollowsPutAndDelete(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	put(t, s, "b", "k", []byte("one"))
	indexMatchesHead(t, s, "b", "k")
	s.now = func() time.Time { return fixedTime.Add(time.Minute) }
	put(t, s, "b", "k", []byte("a longer two"))
	indexMatchesHead(t, s, "b", "k")
	if err := s.Delete("b", "k"); err != nil {
		t.Fatal(err)
	}
	indexMatchesHead(t, s, "b", "k")
	if err := s.Delete("b", "missing"); err != nil {
		t.Fatal(err)
	}
	indexMatchesHead(t, s, "b", "missing")
}

// TestIndexFollowsFailedWrites injects failures at the points where a record
// file changes and checks that the index still matches Head afterwards.
func TestIndexFollowsFailedWrites(t *testing.T) {
	t.Parallel()
	metaFile := func(root string) string { return filepath.Join(root, "meta", "*") }
	metaDir := func(root string) string { return filepath.Join(root, "meta") }
	tests := []struct {
		name      string
		overwrite bool
		faults    func(root string) []fsx.Fault
		present   bool // whether the key must be in the index afterwards
	}{
		{"record rename fails, new key", false, func(string) []fsx.Fault {
			return []fsx.Fault{{Op: fsx.OpRename, Nth: 2, Err: errInjected}}
		}, false},
		{"record rename fails, overwrite", true, func(string) []fsx.Fault {
			return []fsx.Fault{{Op: fsx.OpRename, Nth: 2, Err: errInjected}}
		}, true},
		{"meta sync fails, rollback removes", false, func(root string) []fsx.Fault {
			return []fsx.Fault{{Op: fsx.OpDirSync, PathPattern: metaDir(root), Err: errInjected}}
		}, false},
		{"meta sync fails, rollback restores", true, func(root string) []fsx.Fault {
			return []fsx.Fault{{Op: fsx.OpDirSync, PathPattern: metaDir(root), Err: errInjected}}
		}, true},
		{"meta sync and rollback removal fail", false, func(root string) []fsx.Fault {
			return []fsx.Fault{
				{Op: fsx.OpDirSync, PathPattern: metaDir(root), Err: errInjected},
				{Op: fsx.OpRemove, PathPattern: metaFile(root), Err: errInjected},
			}
		}, true},
		{"meta sync and rollback rename fail", true, func(root string) []fsx.Fault {
			return []fsx.Fault{
				{Op: fsx.OpDirSync, PathPattern: metaDir(root), Err: errInjected},
				{Op: fsx.OpRename, Nth: 3, Err: errInjected},
			}
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			if tt.overwrite {
				put(t, s, "b", "k", []byte("previous"))
			}
			s.fsys = fsx.NewFaulty(fsx.OS{}, tt.faults(root)...)
			if _, err := putString(s, "new body"); err == nil {
				t.Fatal("Put succeeded despite the fault")
			}
			s.fsys = fsx.OS{}
			if _, ok := s.index.get("b", "k"); ok != tt.present {
				t.Fatalf("in index = %v, want %v", ok, tt.present)
			}
			indexMatchesHead(t, s, "b", "k")
		})
	}
}

func TestIndexFollowsFailedDeletes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		fault   func(root string) fsx.Fault
		present bool
	}{
		{"record removal fails", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "meta", "*"), Err: errInjected}
		}, true},
		{"meta sync after removal fails", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpDirSync, PathPattern: filepath.Join(root, "meta"), Err: errInjected}
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			put(t, s, "b", "k", []byte("x"))
			s.fsys = fsx.NewFaulty(fsx.OS{}, tt.fault(root))
			if err := s.Delete("b", "k"); err == nil {
				t.Fatal("Delete succeeded despite the fault")
			}
			s.fsys = fsx.OS{}
			if _, ok := s.index.get("b", "k"); ok != tt.present {
				t.Fatalf("in index = %v, want %v", ok, tt.present)
			}
			indexMatchesHead(t, s, "b", "k")
		})
	}
}

func TestOpenRebuildsIndex(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	keys := []string{"z", "a/b", "é", "a"}
	for i, k := range keys {
		s.now = func() time.Time { return fixedTime.Add(time.Duration(i) * time.Second) }
		put(t, s, "b", k, []byte(k+" body"))
	}
	put(t, s, "other", "k", []byte("x"))

	r := reopen(t, root)
	if got := r.index.keys("b"); !slices.Equal(got, []string{"a", "a/b", "z", "é"}) {
		t.Fatalf("rebuilt keys = %q", got)
	}
	for _, k := range keys {
		indexMatchesHead(t, r, "b", k)
	}
	indexMatchesHead(t, r, "other", "k")
}

func TestOpenSkipsBadRecords(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	put(t, s, "b", "good", []byte("x"))
	put(t, s, "b", "corrupt", []byte("y"))
	meta := filepath.Join(root, "meta")
	if err := os.WriteFile(filepath.Join(meta, recordName("b", "corrupt")+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A valid record under a file name that is not its key's hash.
	raw, err := os.ReadFile(filepath.Join(meta, recordName("b", "good")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, recordName("b", "elsewhere")+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "stray.tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(meta, "ghost.json")); err != nil {
		t.Fatal(err)
	}

	r := reopen(t, root)
	if got := r.index.keys("b"); !slices.Equal(got, []string{"good"}) {
		t.Fatalf("rebuilt keys = %q, want only good", got)
	}
}

func TestOpenIndexIOErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		op, rel string
	}{
		{fsx.OpReadDir, "meta"},
		{fsx.OpOpen, "meta/*"},
		{fsx.OpRead, "meta/*"},
	} {
		t.Run(tt.op, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			put(t, s, "b", "k", []byte("x"))
			fault := fsx.Fault{Op: tt.op, PathPattern: filepath.Join(root, filepath.FromSlash(tt.rel)), Err: errInjected}
			if _, err := Open(fsx.NewFaulty(fsx.OS{}, fault), root); !errors.Is(err, errInjected) {
				t.Fatalf("Open = %v, want the injected error", err)
			}
		})
	}
}
