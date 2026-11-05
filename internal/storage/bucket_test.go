package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

func TestValidBucketName(t *testing.T) {
	t.Parallel()
	valid := []string{
		"abc",
		strings.Repeat("a", 63),
		"demo",
		"my-bucket",
		"my.bucket",
		"a-b.c-d",
		"123",
		"1bucket",
		"bucket1",
		"1.2.3",     // three numbers are not an IPv4 address
		"1.2.3.4.5", // nor are five
		"256.1.1.a",
		"xn-abc",       // only the full xn-- prefix is reserved
		"sthree",       // only the full sthree- prefix is reserved
		"abc-s3alia",   // not the full suffix
		"abc-ol-s3",    // single hyphen is fine
		"abc.mrapx",    // .mrap only as a suffix
		"abc-x-s3",     // single hyphen is fine
		"abc-table-s3", // single hyphen is fine
	}
	for _, name := range valid {
		if !ValidBucketName(name) {
			t.Errorf("ValidBucketName(%q) = false, want true", name)
		}
	}

	invalid := []string{
		"",
		"ab",                    // too short
		strings.Repeat("a", 64), // too long
		"Demo",                  // uppercase
		"my_bucket",             // underscore
		"my bucket",             // space
		"bücket",                // non-ASCII
		"-abc",                  // starts with hyphen
		".abc",                  // starts with dot
		"abc-",                  // ends with hyphen
		"abc.",                  // ends with dot
		"a..b",                  // adjacent dots
		"192.168.5.4",           // IPv4 form
		"0.0.0.0",
		"999.999.999.999", // IPv4 form even if out of range
		"xn--abc",
		"sthree-abc",
		"amzn-s3-demo-bucket",
		"abc-s3alias",
		"abc--ol-s3",
		"abc.mrap",
		"abc--x-s3",
		"abc--table-s3",
		"a/b",
		"a:b",
	}
	for _, name := range invalid {
		if ValidBucketName(name) {
			t.Errorf("ValidBucketName(%q) = true, want false", name)
		}
	}
}

// hasBucket reports whether s holds name in memory.
func hasBucket(s *Store, name string) bool {
	s.bmu.RLock()
	defer s.bmu.RUnlock()
	_, ok := s.buckets[name]
	return ok
}

func TestOpenCreatesBucketsDir(t *testing.T) {
	t.Parallel()
	_, root := newStore(t, nil)
	fi, err := os.Stat(filepath.Join(root, "buckets"))
	if err != nil || !fi.IsDir() {
		t.Fatalf("buckets: (%v, %v), want directory", fi, err)
	}
}

func TestCreateBucket(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	b, err := s.CreateBucket("demo")
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	want := Bucket{Name: "demo", CreationDate: fixedTime.UTC()}
	if b != want || b.CreationDate.Location() != time.UTC {
		t.Fatalf("CreateBucket = %+v, want %+v in UTC", b, want)
	}
	if !hasBucket(s, "demo") {
		t.Fatal("bucket not visible after create")
	}
	if got := entries(t, root, "tmp"); len(got) != 0 {
		t.Fatalf("tmp = %v, want empty", got)
	}

	raw, err := os.ReadFile(filepath.Join(root, "buckets", "demo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("record %q: %v", raw, err)
	}
	if rec["v"] != 1.0 || rec["name"] != "demo" || rec["created"] != fixedTime.UTC().Format(time.RFC3339Nano) || len(rec) != 3 {
		t.Fatalf("record = %v", rec)
	}

	// Re-create keeps the original bucket.
	s.now = func() time.Time { return fixedTime.Add(time.Hour) }
	if _, err := s.CreateBucket("demo"); !errors.Is(err, ErrBucketExists) {
		t.Fatalf("re-create = %v, want ErrBucketExists", err)
	}
	if got := s.buckets["demo"]; got != want {
		t.Fatalf("after re-create = %+v, want %+v", got, want)
	}

	// The bucket survives a reopen with the same creation date.
	r := reopen(t, root)
	if got := r.buckets["demo"]; !got.CreationDate.Equal(want.CreationDate) || got.Name != "demo" || got.CreationDate.Location() != time.UTC {
		t.Fatalf("after reopen = %+v, want %+v", got, want)
	}
	if _, err := r.CreateBucket("demo"); !errors.Is(err, ErrBucketExists) {
		t.Fatalf("re-create after reopen = %v, want ErrBucketExists", err)
	}
}

func TestCreateBucketInvalidName(t *testing.T) {
	t.Parallel()
	fsys := fsx.NewFaulty(fsx.OS{})
	s, _ := newStore(t, nil)
	s.fsys = fsys
	for _, name := range []string{"ab", "Bad_Name", "../x", "a/b"} {
		if _, err := s.CreateBucket(name); !errors.Is(err, ErrInvalidBucketName) {
			t.Fatalf("CreateBucket(%q) = %v, want ErrInvalidBucketName", name, err)
		}
	}
	if h := fsys.History(); len(h) != 0 {
		t.Fatalf("invalid names touched disk: %v", h)
	}
}

func TestCreateBucketConcurrent(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	const n = 16
	errs := make(chan error, n)
	var start sync.WaitGroup
	start.Add(1)
	for range n {
		go func() {
			start.Wait()
			_, err := s.CreateBucket("race")
			errs <- err
		}()
	}
	start.Done()
	ok := 0
	for range n {
		switch err := <-errs; {
		case err == nil:
			ok++
		case !errors.Is(err, ErrBucketExists):
			t.Fatalf("CreateBucket = %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d creates succeeded, want exactly 1", ok)
	}
}

func TestHeadBucket(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	created, err := s.CreateBucket("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.HeadBucket("demo"); err != nil || got != created {
		t.Fatalf("HeadBucket(demo) = %+v, %v; want %+v", got, err, created)
	}
	if _, err := s.HeadBucket("missing"); !errors.Is(err, ErrNoSuchBucket) {
		t.Fatalf("HeadBucket(missing) = %v, want ErrNoSuchBucket", err)
	}
	if _, err := s.HeadBucket("Bad_Name"); !errors.Is(err, ErrInvalidBucketName) {
		t.Fatalf("HeadBucket(Bad_Name) = %v, want ErrInvalidBucketName", err)
	}
}

func TestListBuckets(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	if got := s.ListBuckets(); got == nil || len(got) != 0 {
		t.Fatalf("ListBuckets on an empty store = %#v, want an empty slice", got)
	}
	for i, name := range []string{"beta", "alpha", "gamma"} {
		s.now = func() time.Time { return fixedTime.Add(time.Duration(i) * time.Minute) }
		if _, err := s.CreateBucket(name); err != nil {
			t.Fatal(err)
		}
	}
	got := s.ListBuckets()
	want := []Bucket{
		{"alpha", fixedTime.Add(time.Minute).UTC()},
		{"beta", fixedTime.UTC()},
		{"gamma", fixedTime.Add(2 * time.Minute).UTC()},
	}
	if len(got) != len(want) {
		t.Fatalf("ListBuckets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListBuckets[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	got[0].Name = "changed"
	if s.ListBuckets()[0].Name != "alpha" {
		t.Fatal("ListBuckets returned the store's own data")
	}
}
