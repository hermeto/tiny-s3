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

var errInjected = errors.New("injected")

// fixedTime is the clock every test store uses, in a non-UTC zone so tests
// prove LastModified is converted to UTC.
var fixedTime = time.Date(2026, 11, 4, 9, 30, 15, 123456789, time.FixedZone("EST", -5*3600))

// newStore opens a store on a fresh temp dir through fsys (fsx.OS when nil).
func newStore(t *testing.T, fsys fsx.FS) (*Store, string) {
	t.Helper()
	if fsys == nil {
		fsys = fsx.OS{}
	}
	root := t.TempDir()
	s, err := Open(fsys, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.now = func() time.Time { return fixedTime }
	return s, root
}

// entries lists the names in root/dir, failing the test on error.
func entries(t *testing.T, root, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(des))
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

func TestOpenCreatesLayout(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "store")
	if _, err := Open(fsx.OS{}, root); err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, dir := range []string{"tmp", "data", "meta"} {
		fi, err := os.Stat(filepath.Join(root, dir))
		if err != nil || !fi.IsDir() {
			t.Fatalf("%s: (%v, %v), want directory", dir, fi, err)
		}
	}
}

func TestOpenRemovesTmpLeftovers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"put-1", "meta-2"} {
		if err := os.WriteFile(filepath.Join(root, "tmp", name), []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(fsx.OS{}, root); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := entries(t, root, "tmp"); len(got) != 0 {
		t.Fatalf("tmp after Open = %v, want empty", got)
	}
}

func TestOpenErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		fault fsx.Fault
	}{
		{"mkdir tmp", fsx.Fault{Op: fsx.OpMkdirAll, Nth: 1, Err: errInjected}},
		{"mkdir data", fsx.Fault{Op: fsx.OpMkdirAll, Nth: 2, Err: errInjected}},
		{"mkdir meta", fsx.Fault{Op: fsx.OpMkdirAll, Nth: 3, Err: errInjected}},
		{"list tmp", fsx.Fault{Op: fsx.OpReadDir, Err: errInjected}},
		{"remove leftover", fsx.Fault{Op: fsx.OpRemove, Err: errInjected}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "tmp", "put-1"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := Open(fsx.NewFaulty(fsx.OS{}, tt.fault), root)
			if !errors.Is(err, errInjected) || s != nil {
				t.Fatalf("Open = (%v, %v), want injected error", s, err)
			}
		})
	}
}

func TestValidateKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want error
	}{
		{"k", nil},
		{"a/../b c/ü", nil},
		{strings.Repeat("k", MaxKeyLength), nil},
		{"", ErrInvalidArgument},
		{"bad\xff", ErrInvalidArgument},
		{strings.Repeat("k", MaxKeyLength+1), ErrKeyTooLong},
	}
	for _, tt := range tests {
		if err := validateKey(tt.key); !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
			t.Errorf("validateKey(%.20q) = %v, want %v", tt.key, err, tt.want)
		}
	}
}

func TestValidateSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		size int64
		want error
	}{
		{0, nil},
		{MaxObjectSize, nil},
		{-1, ErrInvalidArgument},
		{MaxObjectSize + 1, ErrEntityTooLarge},
	}
	for _, tt := range tests {
		if err := validateSize(tt.size); !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
			t.Errorf("validateSize(%d) = %v, want %v", tt.size, err, tt.want)
		}
	}
}

func TestRecordNameIsFixedAndDistinct(t *testing.T) {
	t.Parallel()
	a := recordName("b1", "../../etc/passwd")
	b := recordName("b2", "../../etc/passwd")
	if a == b || len(a) != 64 || strings.ContainsAny(a, "/.") {
		t.Fatalf("recordName = %q, %q; want distinct 64-hex names", a, b)
	}
	if recordName("a", "b/c") == recordName("a/b", "c") {
		t.Fatal("bucket/key boundary is ambiguous")
	}
}
