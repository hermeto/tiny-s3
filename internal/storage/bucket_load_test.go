package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

// writeBucketEntry creates root/buckets/name with content.
func writeBucketEntry(t *testing.T, root, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, "buckets")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenRejectsBadBucketRecords(t *testing.T) {
	t.Parallel()
	const created = `"created":"2026-11-04T14:30:15Z"`
	tests := map[string]struct{ file, content string }{
		"invalid json":      {"demo.json", "{"},
		"unknown version":   {"demo.json", `{"v":2,"name":"demo",` + created + `}`},
		"name mismatch":     {"demo.json", `{"v":1,"name":"other",` + created + `}`},
		"zero created":      {"demo.json", `{"v":1,"name":"demo"}`},
		"oversized":         {"demo.json", `{"v":1,"name":"` + strings.Repeat("d", maxRecordSize) + `"}`},
		"invalid file name": {"Bad_Name.json", `{"v":1,"name":"Bad_Name",` + created + `}`},
		"missing .json":     {"demo", `{"v":1,"name":"demo",` + created + `}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := writeBucketEntry(t, root, tt.file, tt.content)
			_, err := Open(fsx.OS{}, root)
			if !errors.Is(err, ErrCorruptRecord) || !strings.Contains(err.Error(), path) {
				t.Fatalf("Open = %v, want ErrCorruptRecord naming %s", err, path)
			}
		})
	}
}

func TestOpenLoadsBuckets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeBucketEntry(t, root, "alpha.json", `{"v":1,"name":"alpha","created":"2026-11-04T14:30:15.123456789Z"}`)
	writeBucketEntry(t, root, "beta.json", `{"v":1,"name":"beta","created":"2026-11-04T09:30:15-05:00"}`)
	s, err := Open(fsx.OS{}, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(s.buckets) != 2 {
		t.Fatalf("buckets = %v, want 2", s.buckets)
	}
	beta := s.buckets["beta"]
	if beta.CreationDate.Location() != fixedTime.UTC().Location() || beta.CreationDate.Hour() != 14 {
		t.Fatalf("beta = %+v, want 14:30:15 UTC", beta)
	}
}

func TestOpenBucketIOErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ op, rel string }{
		"readdir": {fsx.OpReadDir, "buckets"},
		"open":    {fsx.OpOpen, "buckets/demo.json"},
		"read":    {fsx.OpRead, "buckets/demo.json"},
		"mkdir":   {fsx.OpMkdirAll, "buckets"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			fault := fsx.Fault{Op: tt.op, PathPattern: filepath.Join(root, tt.rel), Nth: 1, Err: errInjected}
			writeBucketEntry(t, root, "demo.json", `{"v":1,"name":"demo","created":"2026-11-04T14:30:15Z"}`)
			_, err := Open(fsx.NewFaulty(fsx.OS{}, fault), root)
			if !errors.Is(err, errInjected) {
				t.Fatalf("Open = %v, want injected error", err)
			}
			if name == "open" || name == "read" {
				if want := filepath.Join(root, "buckets", "demo.json"); !strings.Contains(err.Error(), want) {
					t.Fatalf("Open = %v, want it to name %s", err, want)
				}
			}
		})
	}
}

func TestOpenIgnoresBucketRecordCloseError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeBucketEntry(t, root, "demo.json", `{"v":1,"name":"demo","created":"2026-11-04T14:30:15Z"}`)
	fault := fsx.Fault{Op: fsx.OpClose, PathPattern: filepath.Join(root, "buckets", "demo.json"), Nth: 1, Err: errInjected}
	s, err := Open(fsx.NewFaulty(fsx.OS{}, fault), root)
	if err != nil {
		t.Fatalf("Open = %v, want success", err)
	}
	if !hasBucket(s, "demo") {
		t.Fatal("bucket not loaded")
	}
}
