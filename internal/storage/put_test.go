package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func md5hex(b []byte) string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

// put stores body under bucket/key with its exact length and no digest.
func put(t *testing.T, s *Store, bucket, key string, body []byte) Info {
	t.Helper()
	info, err := s.Put(context.Background(), bucket, key, bytes.NewReader(body), int64(len(body)), PutOptions{})
	if err != nil {
		t.Fatalf("Put(%q, %q): %v", bucket, key, err)
	}
	return info
}

// readAll reads the object through Get and closes the reader.
func readAll(t *testing.T, s *Store, bucket, key string) (Info, []byte) {
	t.Helper()
	info, r, err := s.Get(bucket, key)
	if err != nil {
		t.Fatalf("Get(%q, %q): %v", bucket, key, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	return info, b
}

func TestPutHeadGetRoundTrip(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	keys := []string{"hello.txt", "a/b/c", "../../escape", "with space", "ünïcødé/文件", strings.Repeat("k", MaxKeyLength)}
	for i, key := range keys {
		body := bytes.Repeat([]byte{byte('a' + i)}, 100_000+i)
		info := put(t, s, "bucket", key, body)
		want := Info{Bucket: "bucket", Key: key, Size: int64(len(body)), MD5: md5hex(body), LastModified: fixedTime.UTC(), ContentType: DefaultContentType}
		if !reflect.DeepEqual(info, want) {
			t.Fatalf("Put info = %+v, want %+v", info, want)
		}
		if info.LastModified.Location() != time.UTC {
			t.Fatalf("LastModified location = %v, want UTC", info.LastModified.Location())
		}
		head, err := s.Head("bucket", key)
		if err != nil || !head.LastModified.Equal(want.LastModified) || head.Size != want.Size || head.MD5 != want.MD5 || head.Key != key {
			t.Fatalf("Head = (%+v, %v), want %+v", head, err, want)
		}
		got, b := readAll(t, s, "bucket", key)
		if !got.LastModified.Equal(want.LastModified) || got.MD5 != want.MD5 || !bytes.Equal(b, body) {
			t.Fatalf("Get = %+v, %d bytes; want %+v", got, len(b), want)
		}
	}
	metas := entries(t, root, "meta")
	if len(metas) != len(keys) || len(entries(t, root, "data")) != len(keys) || len(entries(t, root, "tmp")) != 0 {
		t.Fatalf("layout: meta %v, data %v, tmp %v", metas, entries(t, root, "data"), entries(t, root, "tmp"))
	}
	for _, key := range keys {
		if !slices.Contains(metas, recordName("bucket", key)+".json") {
			t.Fatalf("meta/ has no record for %q: %v", key, metas)
		}
	}
}

func TestPutZeroLength(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	info := put(t, s, "b", "empty", nil)
	if info.Size != 0 || info.MD5 != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("Put empty = %+v", info)
	}
	if _, b := readAll(t, s, "b", "empty"); len(b) != 0 {
		t.Fatalf("Get empty = %q", b)
	}
}

func TestBucketsAreSeparateNamespaces(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	put(t, s, "one", "k", []byte("first"))
	put(t, s, "two", "k", []byte("second"))
	if _, b := readAll(t, s, "one", "k"); string(b) != "first" {
		t.Fatalf("one/k = %q", b)
	}
	if _, b := readAll(t, s, "two", "k"); string(b) != "second" {
		t.Fatalf("two/k = %q", b)
	}
}

func TestMissingKey(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	if _, err := s.Head("b", "missing"); !errors.Is(err, ErrNoSuchKey) {
		t.Fatalf("Head missing: %v", err)
	}
	if _, r, err := s.Get("b", "missing"); !errors.Is(err, ErrNoSuchKey) || r != nil {
		t.Fatalf("Get missing = (%v, %v)", r, err)
	}
}

func TestOverwriteReplacesAndRemovesOldData(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	put(t, s, "b", "k", []byte("old"))
	put(t, s, "b", "k", []byte("new"))
	if _, b := readAll(t, s, "b", "k"); string(b) != "new" {
		t.Fatalf("after overwrite = %q", b)
	}
	if d := entries(t, root, "data"); len(d) != 1 {
		t.Fatalf("data after overwrite = %v, want one version", d)
	}
}

func TestInvalidArgumentsWriteNothing(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	long := strings.Repeat("k", MaxKeyLength+1)
	tests := []struct {
		name string
		key  string
		size int64
		want error
	}{
		{"empty key", "", 0, ErrInvalidArgument},
		{"invalid utf-8", "\xff", 0, ErrInvalidArgument},
		{"long key", long, 0, ErrKeyTooLong},
		{"negative size", "k", -1, ErrInvalidArgument},
		{"too large", "k", MaxObjectSize + 1, ErrEntityTooLarge},
	}
	for _, tt := range tests {
		if _, err := s.Put(context.Background(), "b", tt.key, strings.NewReader(""), tt.size, PutOptions{}); !errors.Is(err, tt.want) {
			t.Errorf("%s: Put = %v, want %v", tt.name, err, tt.want)
		}
		if tt.size != 0 {
			continue
		}
		if _, err := s.Head("b", tt.key); !errors.Is(err, tt.want) {
			t.Errorf("%s: Head = %v, want %v", tt.name, err, tt.want)
		}
		if _, _, err := s.Get("b", tt.key); !errors.Is(err, tt.want) {
			t.Errorf("%s: Get = %v, want %v", tt.name, err, tt.want)
		}
	}
	for _, dir := range []string{"tmp", "data", "meta"} {
		if e := entries(t, root, dir); len(e) != 0 {
			t.Fatalf("%s = %v, want empty", dir, e)
		}
	}
}

// errReader returns n bytes of 'x' and then err.
type errReader struct {
	n   int
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, r.err
	}
	n := min(len(p), r.n)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.n -= n
	return n, nil
}

func TestPutRejectsBadInputWithoutTrace(t *testing.T) {
	t.Parallel()
	good := []byte("hello")
	sum := md5.Sum(good)
	wrong := md5.Sum([]byte("other"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		body io.Reader
		size int64
		md5  []byte
		want error
	}{
		{"short body", context.Background(), strings.NewReader("hell"), 5, nil, ErrIncompleteBody},
		{"long body", context.Background(), strings.NewReader("hello!"), 5, nil, ErrIncompleteBody},
		{"empty declared, one byte sent", context.Background(), strings.NewReader("x"), 0, nil, ErrIncompleteBody},
		{"bad digest", context.Background(), bytes.NewReader(good), 5, wrong[:], ErrBadDigest},
		{"reader error mid-stream", context.Background(), &errReader{n: 2, err: errInjected}, 5, nil, errInjected},
		{"reader error on probe", context.Background(), &errReader{n: 5, err: errInjected}, 5, nil, errInjected},
		{"cancelled context", cancelled, bytes.NewReader(good), 5, nil, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, root := newStore(t, nil)
			prev := put(t, s, "b", "k", []byte("previous"))
			_, err := s.Put(tt.ctx, "b", "k", tt.body, tt.size, PutOptions{ContentMD5: tt.md5})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Put = %v, want %v", err, tt.want)
			}
			if head, err := s.Head("b", "k"); err != nil || !reflect.DeepEqual(head, prev) {
				t.Fatalf("previous version = (%+v, %v), want %+v", head, err, prev)
			}
			if _, b := readAll(t, s, "b", "k"); string(b) != "previous" {
				t.Fatalf("previous body = %q", b)
			}
			if tmp, data := entries(t, root, "tmp"), entries(t, root, "data"); len(tmp) != 0 || len(data) != 1 {
				t.Fatalf("after rejected Put: tmp %v, data %v; want empty tmp, one data file", tmp, data)
			}
		})
	}
	t.Run("matching digest", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t, nil)
		if _, err := s.Put(context.Background(), "b", "k", bytes.NewReader(good), 5, PutOptions{ContentMD5: sum[:]}); err != nil {
			t.Fatalf("Put with matching Content-MD5: %v", err)
		}
	})
}

func TestPutContentTypeAndMetadata(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t, nil)
	opts := PutOptions{ContentType: "text/plain", Metadata: map[string]string{"owner": "alice", "team": "a,b"}}
	info, err := s.Put(context.Background(), "b", "k", strings.NewReader("hello"), 5, opts)
	if err != nil {
		t.Fatal(err)
	}
	if info.ContentType != "text/plain" || !maps.Equal(info.Metadata, opts.Metadata) {
		t.Fatalf("Put info = %+v", info)
	}
	head, err := s.Head("b", "k")
	if err != nil || !reflect.DeepEqual(head, info) {
		t.Fatalf("Head = %+v, %v; want %+v", head, err, info)
	}
	got, _ := readAll(t, s, "b", "k")
	if !reflect.DeepEqual(got, info) {
		t.Fatalf("Get = %+v, want %+v", got, info)
	}

	// An overwrite without metadata replaces it and gets the default type.
	info = put(t, s, "b", "k", []byte("again"))
	if info.ContentType != DefaultContentType || info.Metadata != nil {
		t.Fatalf("overwrite info = %+v", info)
	}
	if head, _ := s.Head("b", "k"); head.ContentType != DefaultContentType || head.Metadata != nil {
		t.Fatalf("overwrite Head = %+v", head)
	}
}

func TestRecordWithoutContentType(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	put(t, s, "b", "k", []byte("x"))
	path := filepath.Join(root, "meta", recordName("b", "k")+".json")
	var rec map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	delete(rec, "content_type")
	delete(rec, "metadata")
	raw, _ = json.Marshal(rec)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	head, err := s.Head("b", "k")
	if err != nil || head.ContentType != DefaultContentType || head.Metadata != nil {
		t.Fatalf("Head = %+v, %v; want default type and no metadata", head, err)
	}
}

// earlyEOF yields its bytes, then io.ErrUnexpectedEOF, as net/http does for a
// body that ends before its Content-Length.
type earlyEOF struct{ r io.Reader }

func (e *earlyEOF) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func TestPutUnexpectedEOFIsIncompleteBody(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	_, err := s.Put(context.Background(), "b", "k", &earlyEOF{strings.NewReader("short")}, 100, PutOptions{})
	if !errors.Is(err, ErrIncompleteBody) {
		t.Fatalf("Put = %v, want ErrIncompleteBody", err)
	}
	for _, dir := range []string{"tmp", "data", "meta"} {
		if got := entries(t, root, dir); len(got) != 0 {
			t.Fatalf("%s = %v, want empty", dir, got)
		}
	}
}

func TestPutVerify(t *testing.T) {
	t.Parallel()
	s, root := newStore(t, nil)
	prev := put(t, s, "b", "k", []byte("previous"))

	calls := 0
	errVerify := errors.New("verify failed")
	r := strings.NewReader("new body")
	opts := PutOptions{Verify: func() error {
		calls++
		if r.Len() != 0 {
			t.Error("Verify called before the body was read")
		}
		return errVerify
	}}
	if _, err := s.Put(context.Background(), "b", "k", r, 8, opts); !errors.Is(err, errVerify) {
		t.Fatalf("Put = %v, want the Verify error", err)
	}
	if calls != 1 {
		t.Fatalf("Verify called %d times, want 1", calls)
	}
	if head, err := s.Head("b", "k"); err != nil || !reflect.DeepEqual(head, prev) {
		t.Fatalf("Head = %+v, %v; want the previous object", head, err)
	}
	if tmp, data := entries(t, root, "tmp"), entries(t, root, "data"); len(tmp) != 0 || len(data) != 1 {
		t.Fatalf("tmp = %v, data = %v; want empty and the previous version only", tmp, data)
	}

	// A passing Verify commits.
	opts.Verify = func() error { return nil }
	if _, err := s.Put(context.Background(), "b", "k", strings.NewReader("new body"), 8, opts); err != nil {
		t.Fatal(err)
	}
}
