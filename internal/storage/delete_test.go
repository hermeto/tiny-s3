package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

func TestDeleteIsIdempotent(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	put(t, s, "b", "k", []byte("x"))
	for i := range 2 {
		if err := s.Delete("b", "k"); err != nil {
			t.Fatalf("Delete #%d: %v", i+1, err)
		}
		if _, err := s.Head("b", "k"); !errors.Is(err, ErrNoSuchKey) {
			t.Fatalf("Head after Delete #%d = %v", i+1, err)
		}
	}
	if err := s.Delete("b", "never-stored"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
	for _, dir := range []string{"tmp", "data", "meta"} {
		if e := entries(t, root, dir); len(e) != 0 {
			t.Fatalf("%s = %v, want empty", dir, e)
		}
	}
}

func TestDeleteInvalidKey(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	if err := s.Delete("b", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Delete empty key = %v", err)
	}
	if err := s.Delete("b", strings.Repeat("k", MaxKeyLength+1)); !errors.Is(err, ErrKeyTooLong) {
		t.Fatalf("Delete long key = %v", err)
	}
}

func TestDeleteCorruptRecord(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	if err := os.WriteFile(filepath.Join(root, "meta", recordName("b", "k")+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("b", "k"); err != nil {
		t.Fatalf("Delete corrupt: %v", err)
	}
	if e := entries(t, root, "meta"); len(e) != 0 {
		t.Fatalf("meta = %v", e)
	}
}

func TestDeleteFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// fault builds the injected fault from the store root.
		fault func(root string) fsx.Fault
		// wantErr: Delete fails; keepsObject: the object stays readable;
		// keepsData: the data file stays on disk.
		wantErr, keepsObject, keepsData bool
	}{
		{"read record", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpOpen, PathPattern: filepath.Join(root, "meta", "*"), Err: errInjected}
		}, true, true, true},
		{"remove record", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "meta", "*"), Err: errInjected}
		}, true, true, true},
		{"open meta dir", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpOpenDir, PathPattern: filepath.Join(root, "meta"), Err: errInjected}
		}, true, false, true},
		{"sync meta dir", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpDirSync, PathPattern: filepath.Join(root, "meta"), Err: errInjected}
		}, true, false, true},
		{"remove data", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "data", "*"), Err: errInjected}
		}, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			put(t, s, "b", "k", []byte("x"))
			s = reopen(t, root, tt.fault(root))
			err := s.Delete("b", "k")
			if tt.wantErr != errors.Is(err, errInjected) {
				t.Fatalf("Delete = %v, want error %v", err, tt.wantErr)
			}
			_, err = reopen(t, root).Head("b", "k")
			if tt.keepsObject != (err == nil) {
				t.Fatalf("Head after Delete = %v, want object kept %v", err, tt.keepsObject)
			}
			if tt.keepsData != (len(entries(t, root, "data")) == 1) {
				t.Fatalf("data = %v, want kept %v", entries(t, root, "data"), tt.keepsData)
			}
		})
	}
}
