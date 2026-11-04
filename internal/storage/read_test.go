package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

// reopen opens root again through a fault-injecting filesystem.
func reopen(t *testing.T, root string, faults ...fsx.Fault) *Store {
	t.Helper()
	s, err := Open(fsx.NewFaulty(fsx.OS{}, faults...), root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s.now = func() time.Time { return fixedTime }
	return s
}

func TestCorruptRecords(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"invalid json":    "{",
		"unknown version": `{"v":2,"bucket":"b","key":"k","data":"x"}`,
		"oversized":       `{"v":1,"key":"` + strings.Repeat("k", maxRecordSize) + `"}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			path := filepath.Join(root, "meta", recordName("b", "k")+".json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Head("b", "k"); !errors.Is(err, ErrCorruptRecord) {
				t.Fatalf("Head = %v, want ErrCorruptRecord", err)
			}
			if _, r, err := s.Get("b", "k"); !errors.Is(err, ErrCorruptRecord) || r != nil {
				t.Fatalf("Get = (%v, %v), want ErrCorruptRecord", r, err)
			}
		})
	}
}

func TestRecordReadFailures(t *testing.T) {
	t.Parallel()
	for _, op := range []string{fsx.OpOpen, fsx.OpRead} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			put(t, s, "b", "k", []byte("x"))
			pattern := filepath.Join(root, "meta", "*")
			s = reopen(t, root, fsx.Fault{Op: op, PathPattern: pattern, Err: errInjected})
			if _, err := s.Head("b", "k"); !errors.Is(err, errInjected) || errors.Is(err, ErrNoSuchKey) {
				t.Fatalf("Head = %v, want injected error", err)
			}
		})
	}
}

func TestGetDataOpenFailureReleasesLock(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	put(t, s, "b", "k", []byte("x"))
	s = reopen(t, root, fsx.Fault{Op: fsx.OpOpen, PathPattern: filepath.Join(root, "data", "*"), Err: errInjected})
	if _, r, err := s.Get("b", "k"); !errors.Is(err, errInjected) || r != nil {
		t.Fatalf("Get = (%v, %v), want injected error", r, err)
	}
	if _, b := readAll(t, s, "b", "k"); string(b) != "x" {
		t.Fatalf("second Get = %q", b)
	}
}
