package fsx_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

var errInjected = errors.New("injected")

func TestOS(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b")
	var fsys fsx.FS = fsx.OS{}

	if err := fsys.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f, err := fsys.CreateTemp(dir, ".probe-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if filepath.Dir(f.Name()) != dir || !strings.HasPrefix(filepath.Base(f.Name()), ".probe-") {
		t.Fatalf("CreateTemp name = %q, want .probe-* in %q", f.Name(), dir)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := fsys.Remove(f.Name()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(f.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists after Remove: %v", err)
	}
}

func TestOSCreateTempError(t *testing.T) {
	t.Parallel()
	f, err := fsx.OS{}.CreateTemp(filepath.Join(t.TempDir(), "missing"), "x-*")
	if err == nil || f != nil {
		t.Fatalf("CreateTemp in a missing dir = (%v, %v), want (nil, error)", f, err)
	}
}

func TestFaultyInjectsPerOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op  string
		run func(fsys fsx.FS, dir string) error
	}{
		{fsx.OpMkdirAll, func(fsys fsx.FS, dir string) error {
			return fsys.MkdirAll(filepath.Join(dir, "sub"), 0o750)
		}},
		{fsx.OpCreateTemp, func(fsys fsx.FS, dir string) error {
			_, err := fsys.CreateTemp(dir, "x-*")
			return err
		}},
		{fsx.OpClose, func(fsys fsx.FS, dir string) error {
			f, err := fsys.CreateTemp(dir, "x-*")
			if err != nil {
				return err
			}
			return f.Close()
		}},
		{fsx.OpRemove, func(fsys fsx.FS, dir string) error {
			f, err := fsys.CreateTemp(dir, "x-*")
			if err != nil {
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			return fsys.Remove(f.Name())
		}},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			matching := fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: tt.op, PathPrefix: dir, Err: errInjected}}}
			if err := tt.run(matching, dir); !errors.Is(err, errInjected) {
				t.Errorf("matching fault: err = %v, want injected", err)
			}

			otherPath := fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: tt.op, PathPrefix: "/elsewhere", Err: errInjected}}}
			if err := tt.run(otherPath, dir); err != nil {
				t.Errorf("fault under another path: err = %v, want pass-through", err)
			}

			otherOp := fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: "other", Err: errInjected}}}
			if err := tt.run(otherOp, dir); err != nil {
				t.Errorf("fault for another op: err = %v, want pass-through", err)
			}
		})
	}
}

func TestFaultyEmptyPrefixMatchesAll(t *testing.T) {
	t.Parallel()
	fsys := fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: fsx.OpMkdirAll, Err: errInjected}}}
	if err := fsys.MkdirAll(filepath.Join(t.TempDir(), "x"), 0o750); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want injected", err)
	}
}

func TestFaultyPassesThroughUnderlyingErrors(t *testing.T) {
	t.Parallel()
	fsys := fsx.Faulty{FS: fsx.OS{}}
	if _, err := fsys.CreateTemp(filepath.Join(t.TempDir(), "missing"), "x-*"); err == nil {
		t.Fatal("CreateTemp in a missing dir succeeded")
	}
}
