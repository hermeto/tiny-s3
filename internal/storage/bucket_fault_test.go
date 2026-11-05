package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

// faultyStore opens a fresh store and then routes its I/O through a Faulty
// with faults, so Open's own operations do not count towards Nth.
func faultyStore(t *testing.T, faults ...fsx.Fault) (*Store, *fsx.Faulty, string) {
	t.Helper()
	s, root := newStore(t, nil)
	fsys := fsx.NewFaulty(fsx.OS{}, faults...)
	s.fsys = fsys
	return s, fsys, root
}

// bucketOnDisk reports whether a reopened store sees name.
func bucketOnDisk(t *testing.T, root, name string) bool {
	t.Helper()
	return hasBucket(reopen(t, root), name)
}

func TestCreateBucketHistory(t *testing.T) {
	t.Parallel()
	s, fsys, root := faultyStore(t)
	if _, err := s.CreateBucket("demo"); err != nil {
		t.Fatal(err)
	}
	tmp, buckets := filepath.Join(root, "tmp"), filepath.Join(root, "buckets")
	type step struct{ op, path string }
	want := []step{
		{fsx.OpCreate, tmp}, {fsx.OpWrite, ""}, {fsx.OpSync, ""}, {fsx.OpClose, ""},
		{fsx.OpRename, ""}, {fsx.OpOpenDir, buckets}, {fsx.OpDirSync, buckets}, {fsx.OpClose, buckets},
	}
	h := fsys.History()
	if len(h) != len(want) {
		t.Fatalf("history has %d operations, want %d: %v", len(h), len(want), h)
	}
	for i, w := range want {
		if h[i].Op != w.op || (w.path != "" && h[i].Path != w.path) {
			t.Fatalf("operation %d = %s %s, want %s %s", i, h[i].Op, h[i].Path, w.op, w.path)
		}
	}
	if !strings.HasPrefix(filepath.Base(h[1].Path), "bucket-") {
		t.Fatalf("temp file %q is not named bucket-*", h[1].Path)
	}
	if !strings.HasPrefix(h[4].Path, tmp) || h[4].OtherPath != filepath.Join(buckets, "demo.json") {
		t.Fatalf("rename = %s -> %s", h[4].Path, h[4].OtherPath)
	}
}

// TestCreateBucketFailsCleanlyAtEveryStep injects a failure at each operation
// of a successful create. Every failure is returned, leaves tmp/ empty, and
// leaves no bucket in memory or on disk: failures after the rename roll the
// record back.
func TestCreateBucketFailsCleanlyAtEveryStep(t *testing.T) {
	t.Parallel()
	probe, fsys, _ := faultyStore(t)
	if _, err := probe.CreateBucket("demo"); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for k, rec := range fsys.History() {
		counts[rec.Op]++
		nth := counts[rec.Op]
		t.Run(fmt.Sprintf("%02d-%s-%d", k, rec.Op, nth), func(t *testing.T) {
			t.Parallel()
			s, _, root := faultyStore(t, fsx.Fault{Op: rec.Op, Nth: nth, Err: errInjected})
			if _, err := s.CreateBucket("demo"); !errors.Is(err, errInjected) {
				t.Fatalf("CreateBucket = %v, want injected error", err)
			}
			if hasBucket(s, "demo") {
				t.Fatal("bucket visible in memory after a failed create")
			}
			if got := entries(t, root, "tmp"); len(got) != 0 {
				t.Fatalf("tmp = %v, want empty", got)
			}
			if got := entries(t, root, "buckets"); len(got) != 0 {
				t.Fatalf("buckets = %v, want empty", got)
			}
			if bucketOnDisk(t, root, "demo") {
				t.Fatal("bucket visible after reopen")
			}
		})
	}
}

// TestCreateBucketRollbackFails covers a buckets/ sync failure after the
// rename whose rollback also fails: both errors are returned and the complete
// record is kept, so the bucket exists in memory and on disk.
func TestCreateBucketRollbackFails(t *testing.T) {
	t.Parallel()
	errRollback := errors.New("rollback")
	tests := map[string]func(root string) []fsx.Fault{
		"remove": func(root string) []fsx.Fault {
			return []fsx.Fault{
				{Op: fsx.OpDirSync, Nth: 1, Err: errInjected},
				{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "buckets", "demo.json"), Nth: 1, Err: errRollback},
			}
		},
		// Faulty counts a call only against the first rule matching its
		// operation, so the rollback's sync fails through its open.
		"second sync": func(string) []fsx.Fault {
			return []fsx.Fault{
				{Op: fsx.OpDirSync, Nth: 1, Err: errInjected},
				{Op: fsx.OpOpenDir, Nth: 2, Err: errRollback},
			}
		},
	}
	for name, faults := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			s.fsys = fsx.NewFaulty(fsx.OS{}, faults(root)...)
			_, err := s.CreateBucket("demo")
			if !errors.Is(err, errInjected) || !errors.Is(err, errRollback) {
				t.Fatalf("CreateBucket = %v, want both errors", err)
			}
			if !hasBucket(s, "demo") {
				t.Fatal("bucket not in memory after a failed rollback")
			}
			if name == "remove" && !bucketOnDisk(t, root, "demo") {
				t.Fatal("record not kept after a failed rollback")
			}
			if _, err := s.CreateBucket("demo"); !errors.Is(err, ErrBucketExists) {
				t.Fatalf("re-create = %v, want ErrBucketExists", err)
			}
		})
	}
}

func TestCreateBucketEncodeError(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	s.now = func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := s.CreateBucket("demo"); err == nil || !strings.Contains(err.Error(), "encode bucket") {
		t.Fatalf("CreateBucket = %v, want encode error", err)
	}
	if hasBucket(s, "demo") {
		t.Fatal("bucket visible after encode error")
	}
	if got := entries(t, root, "tmp"); len(got) != 0 {
		t.Fatalf("tmp = %v, want empty", got)
	}
	if _, err := os.Stat(filepath.Join(root, "buckets", "demo.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record stat = %v, want not exist", err)
	}
}
