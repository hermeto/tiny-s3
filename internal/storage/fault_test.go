package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

const (
	v1 = "version one"
	v2 = "version two, longer"
)

// putString stores body under b/k through s.
func putString(s *Store, body string) (Info, error) {
	return s.Put(context.Background(), "b", "k", strings.NewReader(body), int64(len(body)), nil)
}

func TestPutHistoryFollowsDurabilityOrder(t *testing.T) {
	t.Parallel()
	_, root := newStore(t, nil)
	fsys := fsx.NewFaulty(fsx.OS{})
	s := reopen(t, root)
	s.fsys = fsys
	if _, err := putString(s, v1); err != nil {
		t.Fatal(err)
	}
	tmp, data, meta := filepath.Join(root, "tmp"), filepath.Join(root, "data"), filepath.Join(root, "meta")
	record := filepath.Join(meta, recordName("b", "k")+".json")
	type step struct{ op, path string }
	want := []step{
		{fsx.OpCreate, tmp}, {fsx.OpWrite, ""}, {fsx.OpSync, ""}, {fsx.OpClose, ""},
		{fsx.OpRename, ""}, {fsx.OpOpenDir, data}, {fsx.OpDirSync, data}, {fsx.OpClose, data},
		{fsx.OpCreate, tmp}, {fsx.OpWrite, ""}, {fsx.OpSync, ""}, {fsx.OpClose, ""},
		{fsx.OpOpen, record},
		{fsx.OpRename, ""}, {fsx.OpOpenDir, meta}, {fsx.OpDirSync, meta}, {fsx.OpClose, meta},
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
	if !strings.HasPrefix(h[4].Path, tmp) || filepath.Dir(h[4].OtherPath) != data {
		t.Fatalf("data rename = %s -> %s", h[4].Path, h[4].OtherPath)
	}
	if !strings.HasPrefix(h[13].Path, tmp) || h[13].OtherPath != record {
		t.Fatalf("record rename = %s -> %s", h[13].Path, h[13].OtherPath)
	}
}

// bestEffort reports whether a failure of history record r must not fail the
// Put: removing the previous version's data after the commit, and closing a
// read-only record file.
func bestEffort(root string, r fsx.Record) bool {
	switch r.Op {
	case fsx.OpRemove:
		return filepath.Dir(r.Path) == filepath.Join(root, "data")
	case fsx.OpClose:
		return filepath.Dir(r.Path) == filepath.Join(root, "meta")
	}
	return false
}

// TestPutFailsCleanlyAtEveryStep replays the history of a successful Put and
// injects a failure at each operation in turn.
func TestPutFailsCleanlyAtEveryStep(t *testing.T) {
	t.Parallel()
	for _, overwrite := range []bool{false, true} {
		// setup returns a store root holding v1 when overwriting.
		setup := func(t *testing.T) (string, Info) {
			s, root := newStore(t, nil)
			if !overwrite {
				return root, Info{}
			}
			info, err := putString(s, v1)
			if err != nil {
				t.Fatal(err)
			}
			return root, info
		}

		probeRoot, _ := setup(t)
		probe := fsx.NewFaulty(fsx.OS{})
		ps := reopen(t, probeRoot)
		ps.fsys = probe
		if _, err := putString(ps, v2); err != nil {
			t.Fatal(err)
		}
		history := probe.History()
		counts := map[string]int{}
		for k, rec := range history {
			counts[rec.Op]++
			nth := counts[rec.Op]
			name := fmt.Sprintf("overwrite=%v/%02d-%s-%d", overwrite, k, rec.Op, nth)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				root, prev := setup(t)
				s := reopen(t, root, fsx.Fault{Op: rec.Op, Nth: nth, Err: errInjected})
				_, err := putString(s, v2)
				if bestEffort(probeRoot, history[k]) {
					if err != nil {
						t.Fatalf("Put with best-effort %s failing = %v, want success", rec.Op, err)
					}
					return
				}
				if !errors.Is(err, errInjected) {
					t.Fatalf("Put = %v, want injected error", err)
				}
				clean := reopen(t, root)
				head, err := clean.Head("b", "k")
				switch {
				case overwrite && (err != nil || head != prev):
					t.Fatalf("Head = (%+v, %v), want previous %+v", head, err, prev)
				case !overwrite && !errors.Is(err, ErrNoSuchKey):
					t.Fatalf("Head = (%+v, %v), want ErrNoSuchKey", head, err)
				}
				if overwrite {
					if _, b := readAll(t, clean, "b", "k"); string(b) != v1 {
						t.Fatalf("body = %q, want previous", b)
					}
				}
				wantData := 0
				if overwrite {
					wantData = 1
				}
				if tmp, data := entries(t, root, "tmp"), entries(t, root, "data"); len(tmp) != 0 || len(data) != wantData {
					t.Fatalf("tmp %v, data %v; want empty tmp and %d data files", tmp, data, wantData)
				}
			})
		}
	}
}

func TestPutCleanupFailureKeepsOriginalError(t *testing.T) {
	t.Parallel()
	errRemove := errors.New("remove failed")
	_, root := newStore(t, nil)
	s := reopen(t, root,
		fsx.Fault{Op: fsx.OpWrite, Err: errInjected},
		fsx.Fault{Op: fsx.OpRemove, Err: errRemove})
	_, err := putString(s, v1)
	if !errors.Is(err, errInjected) || errors.Is(err, errRemove) {
		t.Fatalf("Put = %v, want only the write error", err)
	}
}

func TestPutShortWrite(t *testing.T) {
	t.Parallel()
	_, root := newStore(t, nil)
	s := reopen(t, root, fsx.Fault{Op: fsx.OpWrite, WriteCount: 1})
	if _, err := putString(s, v1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Put = %v, want io.ErrShortWrite", err)
	}
	if tmp := entries(t, root, "tmp"); len(tmp) != 0 {
		t.Fatalf("tmp = %v", tmp)
	}
}

func TestPutRecordEncodeFailure(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	s.now = func() time.Time { return time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := putString(s, v1); err == nil || !strings.Contains(err.Error(), "encode record") {
		t.Fatalf("Put = %v, want encode error", err)
	}
	if tmp, data := entries(t, root, "tmp"), entries(t, root, "data"); len(tmp) != 0 || len(data) != 0 {
		t.Fatalf("tmp %v, data %v; want both empty", tmp, data)
	}
}

func TestPutOverwritesCorruptRecord(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	if err := os.WriteFile(filepath.Join(root, "meta", recordName("b", "k")+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := putString(s, v1); err != nil {
		t.Fatalf("Put over corrupt record: %v", err)
	}
	if _, b := readAll(t, s, "b", "k"); string(b) != v1 {
		t.Fatalf("body = %q", b)
	}
}

// TestPutRollbackFailure fails the meta/ sync after the commit rename and
// then fails the rollback. Put must report the error, and whichever version
// the record points at must be complete.
func TestPutRollbackFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		overwrite bool
		rollback  func(root string) fsx.Fault
	}{
		{"remove new record", false, func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "meta", "*"), Err: errInjected}
		}},
		{"sync after removing new record", false, func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpOpenDir, PathPattern: filepath.Join(root, "meta"), Nth: 2, Err: errInjected}
		}},
		{"write previous record", true, func(string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpCreate, Nth: 3, Err: errInjected}
		}},
		{"rename previous record", true, func(string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRename, Nth: 3, Err: errInjected}
		}},
		{"sync after restoring previous record", true, func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpOpenDir, PathPattern: filepath.Join(root, "meta"), Nth: 2, Err: errInjected}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			if tt.overwrite {
				if _, err := putString(s, v1); err != nil {
					t.Fatal(err)
				}
			}
			errSync := errors.New("meta sync failed")
			s = reopen(t, root,
				fsx.Fault{Op: fsx.OpDirSync, PathPattern: filepath.Join(root, "meta"), Err: errSync},
				tt.rollback(root))
			if _, err := putString(s, v2); !errors.Is(err, errSync) || !errors.Is(err, errInjected) {
				t.Fatalf("Put = %v, want sync and rollback errors", err)
			}
			clean := reopen(t, root)
			if _, err := clean.Head("b", "k"); !tt.overwrite && errors.Is(err, ErrNoSuchKey) {
				// The new record was removed; the data stays in case the
				// unsynced removal is lost in a crash.
				return
			}
			info, b := readAll(t, clean, "b", "k")
			if (string(b) != v1 && string(b) != v2) || info.Size != int64(len(b)) || info.MD5 != md5hex(b) {
				t.Fatalf("after failed rollback: %+v, %q; want a complete version", info, b)
			}
		})
	}
}
