package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

func TestRecoveryReportCounts(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	if got := s.Recovery(); got != (RecoveryReport{}) {
		t.Fatalf("fresh store: %+v, want all zero", got)
	}
	for _, b := range []string{"one", "two"} {
		if _, err := s.CreateBucket(b); err != nil {
			t.Fatal(err)
		}
	}
	put(t, s, "one", "a", []byte("x"))
	put(t, s, "one", "b", []byte("y"))
	put(t, s, "two", "a", []byte("z"))

	r, err := Open(fsx.OS{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Recovery(), (RecoveryReport{ObjectsIndexed: 3, Buckets: 2}); got != want {
		t.Fatalf("reopened: %+v, want %+v", got, want)
	}
}

// TestRecoveryCleansLeftovers plants every kind of leftover a crash or an
// outside change can produce and checks what Open removes, keeps and counts.
func TestRecoveryCleansLeftovers(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	put(t, s, "b", "k1", []byte("one"))
	put(t, s, "b", "k2", []byte("two"))
	put(t, s, "b", "corrupt", []byte("c"))
	put(t, s, "b", "gone", []byte("g"))
	meta, data, tmp := filepath.Join(root, "meta"), filepath.Join(root, "data"), filepath.Join(root, "tmp")
	recordOf := func(key string) string { return filepath.Join(meta, recordName("b", key)+".json") }

	// The corrupt record's body can no longer be found: it becomes an orphan.
	if err := os.WriteFile(recordOf("corrupt"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A record whose body disappeared.
	gone, err := s.readRecord(recordName("b", "gone"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(data, gone.Data)); err != nil {
		t.Fatal(err)
	}
	// A valid record under another key's file name (its body is k1's).
	raw, err := os.ReadFile(recordOf("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordOf("misplaced"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(tmp, "put-1"), filepath.Join(tmp, "meta-2"), filepath.Join(data, "orphan-1"), filepath.Join(data, "orphan-2")} {
		if err := os.WriteFile(f, []byte("leftover"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fsys := fsx.NewFaulty(fsx.OS{})
	r, err := Open(fsys, root)
	if err != nil {
		t.Fatal(err)
	}
	want := RecoveryReport{TmpRemoved: 2, OrphansRemoved: 3, ObjectsIndexed: 2, RecordsSkipped: 3}
	if got := r.Recovery(); got != want {
		t.Fatalf("report %+v, want %+v", got, want)
	}
	if got := r.index.keys("b"); !slices.Equal(got, []string{"k1", "k2"}) {
		t.Fatalf("indexed %q", got)
	}
	if got := entries(t, root, "tmp"); len(got) != 0 {
		t.Fatalf("tmp = %v", got)
	}
	var bodies []string
	for _, k := range []string{"k1", "k2"} {
		rec, err := r.readRecord(recordName("b", k))
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, rec.Data)
	}
	slices.Sort(bodies)
	if got := entries(t, root, "data"); !slices.Equal(got, bodies) {
		t.Fatalf("data = %q, want only %q", got, bodies)
	}
	// Records are reported, never deleted by recovery.
	for _, k := range []string{"corrupt", "gone", "misplaced"} {
		if _, err := os.Stat(recordOf(k)); err != nil {
			t.Fatalf("record of %s: %v", k, err)
		}
	}
	// Cleanup is idempotent, so it needs no directory sync.
	for _, rec := range fsys.History() {
		if rec.Op == fsx.OpDirSync {
			t.Fatalf("Open synced %s", rec.Path)
		}
	}
}

func TestRecoveryFailuresNamePath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		plant string // relative path of a leftover to plant
		fault func(root string) fsx.Fault
	}{
		{"list data", "", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpReadDir, PathPattern: filepath.Join(root, "data"), Err: errInjected}
		}},
		{"remove orphan", "data/orphan", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "data", "*"), Err: errInjected}
		}},
		{"remove temp file", "tmp/put-1", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "tmp", "*"), Err: errInjected}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, root := newStore(t, nil)
			path := filepath.Join(root, "data")
			if tt.plant != "" {
				path = filepath.Join(root, filepath.FromSlash(tt.plant))
				if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Open(fsx.NewFaulty(fsx.OS{}, tt.fault(root)), root)
			if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), path) {
				t.Fatalf("Open = %v, want the injected error naming %s", err, path)
			}
		})
	}
}

// scanRecords decodes every record file in root/meta independently of the
// engine and returns, for bucket "b", the entry of each valid record whose
// body exists, and the set of bodies they name.
func scanRecords(t *testing.T, root string) (map[string]entry, map[string]bool) {
	t.Helper()
	entriesByKey := map[string]entry{}
	bodies := map[string]bool{}
	for _, name := range entries(t, root, "meta") {
		raw, err := os.ReadFile(filepath.Join(root, "meta", name))
		if err != nil {
			t.Fatal(err)
		}
		var rec struct {
			V            int `json:"v"`
			Bucket, Key  string
			Size         int64     `json:"size"`
			MD5          string    `json:"md5"`
			LastModified time.Time `json:"last_modified"`
			Data         string    `json:"data"`
		}
		if json.Unmarshal(raw, &rec) != nil || rec.V != 1 || recordName(rec.Bucket, rec.Key)+".json" != name {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "data", rec.Data)); err != nil {
			continue
		}
		entriesByKey[rec.Key] = entry{size: rec.Size, md5: rec.MD5, lastModified: rec.LastModified}
		bodies[rec.Data] = true
	}
	return entriesByKey, bodies
}

// indexEntries returns bucket "b" of the index as a map.
func indexEntries(s *Store) map[string]entry {
	out := map[string]entry{}
	for _, k := range s.index.keys("b") {
		e, _ := s.index.get("b", k)
		out[k] = e
	}
	return out
}

// TestRecoveryAfterRandomFailures runs random writes and deletes with random
// injected faults, then checks the index against an independent scan of the
// records, before and after a reopen, and that the reopen leaves exactly the
// referenced bodies.
func TestRecoveryAfterRandomFailures(t *testing.T) {
	t.Parallel()
	ops := []string{fsx.OpCreate, fsx.OpWrite, fsx.OpSync, fsx.OpClose, fsx.OpRename,
		fsx.OpOpenDir, fsx.OpDirSync, fsx.OpRemove, fsx.OpOpen, fsx.OpRead}
	keys := []string{"a", "b", "c/d", "c/e", "f", "g", "é", "z"}
	for seed := range 20 {
		t.Run(fmt.Sprintf("seed-%02d", seed), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(seed), 2026))
			s, root := newStore(t, nil)
			failed := 0
			for range 200 {
				var faults []fsx.Fault
				if rng.IntN(3) == 0 {
					faults = append(faults, fsx.Fault{Op: ops[rng.IntN(len(ops))], Nth: 1 + rng.IntN(3), Err: errInjected})
				}
				s.fsys = fsx.NewFaulty(fsx.OS{}, faults...)
				key := keys[rng.IntN(len(keys))]
				if rng.IntN(3) == 2 {
					if s.Delete("b", key) != nil {
						failed++
					}
					continue
				}
				body := make([]byte, rng.IntN(2000))
				for i := range body {
					body[i] = byte(rng.IntN(256))
				}
				if _, err := s.Put(context.Background(), "b", key, bytes.NewReader(body), int64(len(body)), PutOptions{}); err != nil {
					failed++
				}
			}
			s.fsys = fsx.OS{}
			// Guard against a test that proves nothing: faults must bite.
			if failed < 10 {
				t.Fatalf("only %d of 200 operations failed; the injected faults are not reaching the engine", failed)
			}
			t.Logf("%d of 200 operations failed", failed)

			want, bodies := scanRecords(t, root)
			if got := indexEntries(s); !maps.Equal(got, want) {
				t.Fatalf("live index %v, records on disk %v", got, want)
			}
			r := reopen(t, root)
			if got := indexEntries(r); !maps.Equal(got, want) {
				t.Fatalf("rebuilt index %v, records on disk %v", got, want)
			}
			if got := entries(t, root, "tmp"); len(got) != 0 {
				t.Fatalf("tmp after reopen = %v", got)
			}
			data := entries(t, root, "data")
			if len(data) != len(bodies) {
				t.Fatalf("data after reopen = %v, want exactly the referenced %v", data, bodies)
			}
			for _, d := range data {
				if !bodies[d] {
					t.Fatalf("unreferenced body %s kept", d)
				}
			}
		})
	}
}
