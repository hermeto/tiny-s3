package s3api

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// readOps returns Ops with PutObject, GetObject and HeadObject over store.
func readOps(store ObjectStore, log *slog.Logger) Ops {
	return Ops{
		PutObject:  PutObjectOp(store, log),
		GetObject:  GetObjectOp(store, log),
		HeadObject: HeadObjectOp(store, log),
	}
}

// readStore opens a real store through fsys (fsx.OS when nil) and returns a
// handler serving the object reads with a readable log.
func readStore(t *testing.T, fsys fsx.FS) (http.Handler, *storage.Store, string, *logBuffer) {
	t.Helper()
	if fsys == nil {
		fsys = fsx.OS{}
	}
	root := t.TempDir()
	store, err := storage.Open(fsys, root)
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	return NewHandler(readOps(store, log), log), store, root, logs
}

func mustPut(t *testing.T, h http.Handler, key, body string, header http.Header) {
	t.Helper()
	if w := serveReq(h, putReq("/demo/"+key, body, header)); w.Code != http.StatusOK {
		t.Fatalf("put %s: %d %s", key, w.Code, w.Body.String())
	}
}

func TestGetObject(t *testing.T) {
	t.Parallel()
	h, store, _, _ := readStore(t, nil)
	mustPut(t, h, "a/b.txt", "hello world", http.Header{
		"Content-Type": {"text/plain"}, "X-Amz-Meta-Owner": {"alice"}, "X-Amz-Meta-Team": {"core"},
	})
	info, err := store.Head("demo", "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}

	w := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/a/b.txt", nil))
	if w.Code != http.StatusOK || w.Body.String() != "hello world" {
		t.Fatalf("GET = %d %q", w.Code, w.Body.String())
	}
	want := map[string]string{
		"ETag":           `"5eb63bbbe01eeed093cb22bb8f5acdc3"`,
		"Content-Length": "11",
		"Content-Type":   "text/plain",
		"Last-Modified":  info.LastModified.UTC().Truncate(time.Second).Format(http.TimeFormat),
		"Accept-Ranges":  "bytes",
	}
	for k, v := range want {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// S3 sends user metadata names in lowercase, and clients keep the case
	// they receive, so these keys must not be canonicalized.
	for k, v := range map[string]string{"x-amz-meta-owner": "alice", "x-amz-meta-team": "core"} {
		if got := w.Header()[k]; len(got) != 1 || got[0] != v {
			t.Errorf("%s = %v, want [%s]; headers %v", k, got, v, w.Header())
		}
	}
	if !strings.HasSuffix(w.Header().Get("Last-Modified"), " GMT") {
		t.Errorf("Last-Modified %q is not GMT", w.Header().Get("Last-Modified"))
	}
}

func TestGetObjectEmptyAndDefaultType(t *testing.T) {
	t.Parallel()
	h, _, _, _ := readStore(t, nil)
	mustPut(t, h, "empty", "", nil)
	w := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/empty", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "0" ||
		w.Header().Get("Content-Type") != storage.DefaultContentType {
		t.Fatalf("GET empty = %d, %q, headers %v", w.Code, w.Body.String(), w.Header())
	}
}

func TestGetObjectMissingKey(t *testing.T) {
	t.Parallel()
	h, _, _, _ := readStore(t, nil)
	wantError(t, serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/missing", nil)), http.MethodGet, ErrNoSuchKey)
}

func TestGetObjectStoreFailureBeforeStatus(t *testing.T) {
	t.Parallel()
	h0, _, root, _ := readStore(t, nil)
	mustPut(t, h0, "k", "hello", nil)
	fault := fsx.Fault{Op: fsx.OpOpen, PathPattern: filepath.Join(root, "data", "*"), Err: errors.New("disk on fire")}
	store, err := storage.Open(fsx.NewFaulty(fsx.OS{}, fault), root)
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	h := NewHandler(readOps(store, log), log)
	wantError(t, serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/k", nil)), http.MethodGet, ErrInternalError)
	if recs := logs.records(t); len(recs) != 1 || recs[0]["op"] != "GetObject" {
		t.Fatalf("log = %v", recs)
	}
}

// TestGetObjectReadFailureAborts fails a read after the status was sent: the
// client must see a broken transfer, never a clean short body.
func TestGetObjectReadFailureAborts(t *testing.T) {
	t.Parallel()
	h0, _, root, _ := readStore(t, nil)
	body := make([]byte, 1<<20)
	_, _ = rand.Read(body)
	mustPut(t, h0, "k", string(body), nil)

	fault := fsx.Fault{Op: fsx.OpRead, PathPattern: filepath.Join(root, "data", "*"), Nth: 2, Err: errors.New("bad sector")}
	store, err := storage.Open(fsx.NewFaulty(fsx.OS{}, fault), root)
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	srv := httptest.NewServer(NewHandler(readOps(store, log), log))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/demo/k")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || err == nil || len(got) >= len(body) {
		t.Fatalf("status %d, read %d bytes, err %v; want 200 then a failed read", resp.StatusCode, len(got), err)
	}
	recs := logs.records(t)
	if len(recs) != 1 || recs[0]["msg"] != "storage_error" || recs[0]["op"] != "GetObject" {
		t.Fatalf("log = %v", recs)
	}
}

// TestGetObjectClientGone checks that a client leaving mid-body does not log
// a storage error.
func TestGetObjectClientGone(t *testing.T) {
	t.Parallel()
	h, _, _, logs := readStore(t, nil)
	body := bytes.Repeat([]byte("x"), 8<<20)
	mustPut(t, h, "k", string(body), nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/demo/k")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadFull(resp.Body, make([]byte, 1024))
	_ = resp.Body.Close() // closes the connection mid-body
	srv.Close()           // waits for the handler to finish
	for _, rec := range logs.records(t) {
		if rec["msg"] == "storage_error" {
			t.Fatalf("logged %v for a client that left", rec)
		}
	}
}

func TestGetObjectPreFeature007Record(t *testing.T) {
	t.Parallel()
	h, _, root, _ := readStore(t, nil)
	mustPut(t, h, "old", "x", http.Header{"Content-Type": {"text/plain"}})
	dir := filepath.Join(root, "meta")
	des, err := os.ReadDir(dir)
	if err != nil || len(des) != 1 {
		t.Fatal(des, err)
	}
	path := filepath.Join(dir, des[0].Name())
	raw, _ := os.ReadFile(path)
	raw = bytes.Replace(raw, []byte(`,"content_type":"text/plain"`), nil, 1)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	w := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/old", nil))
	if w.Header().Get("Content-Type") != storage.DefaultContentType {
		t.Fatalf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
}

func TestHeadObjectMatchesGet(t *testing.T) {
	t.Parallel()
	h, _, _, _ := readStore(t, nil)
	mustPut(t, h, "k", "hello world", http.Header{"Content-Type": {"text/plain"}, "X-Amz-Meta-Owner": {"alice"}})
	get := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/k", nil))
	head := serveReq(h, httptest.NewRequest(http.MethodHead, "/demo/k", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d, body %q", head.Code, head.Body.String())
	}
	for k, v := range get.Header() {
		if k == "X-Amz-Request-Id" {
			continue
		}
		if got := head.Header()[k]; len(got) != len(v) || got[0] != v[0] {
			t.Errorf("HEAD %s = %v, GET has %v", k, got, v)
		}
	}
	if len(head.Header()) != len(get.Header()) {
		t.Errorf("HEAD headers %v, GET headers %v", head.Header(), get.Header())
	}
}

func TestHeadObjectErrors(t *testing.T) {
	t.Parallel()
	h, _, _, _ := readStore(t, nil)
	wantError(t, serveReq(h, httptest.NewRequest(http.MethodHead, "/demo/missing", nil)), http.MethodHead, ErrNoSuchKey)

	h0, _, root, _ := readStore(t, nil)
	mustPut(t, h0, "k", "x", nil)
	fault := fsx.Fault{Op: fsx.OpOpen, PathPattern: filepath.Join(root, "meta", "*"), Err: errors.New("disk on fire")}
	store, err := storage.Open(fsx.NewFaulty(fsx.OS{}, fault), root)
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	h = NewHandler(readOps(store, log), log)
	wantError(t, serveReq(h, httptest.NewRequest(http.MethodHead, "/demo/k", nil)), http.MethodHead, ErrInternalError)
	if recs := logs.records(t); len(recs) != 1 || recs[0]["op"] != "HeadObject" {
		t.Fatalf("log = %v", recs)
	}
}

func TestConditionalResponses(t *testing.T) {
	t.Parallel()
	h, _, _, _ := readStore(t, nil)
	mustPut(t, h, "k", "hello world", nil)
	etag := `"5eb63bbbe01eeed093cb22bb8f5acdc3"`
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, "/demo/k", nil)
		r.Header.Set("If-None-Match", etag)
		w := serveReq(h, r)
		if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("ETag") != etag ||
			w.Header().Get("Last-Modified") == "" || w.Header().Get("Content-Length") != "" || w.Header().Get("Content-Type") != "" {
			t.Fatalf("%s If-None-Match: %d %q %v", method, w.Code, w.Body.String(), w.Header())
		}

		r = httptest.NewRequest(method, "/demo/k", nil)
		r.Header.Set("If-Match", `"0000"`)
		wantError(t, serveReq(h, r), method, ErrPreconditionFailed)

		r = httptest.NewRequest(method, "/demo/missing", nil)
		r.Header.Set("If-Match", etag)
		wantError(t, serveReq(h, r), method, ErrNoSuchKey)

		r = httptest.NewRequest(method, "/demo/k", nil)
		r.Header.Set("If-Match", etag)
		if w := serveReq(h, r); w.Code != http.StatusOK {
			t.Fatalf("%s If-Match same: %d", method, w.Code)
		}
	}
}

// TestGetSurvivesOverwriteAndDelete starts a download, overwrites and
// deletes the key through the engine, then finishes the download.
func TestGetSurvivesOverwriteAndDelete(t *testing.T) {
	t.Parallel()
	h, store, _, _ := readStore(t, nil)
	original := make([]byte, 1<<20)
	_, _ = rand.Read(original)
	mustPut(t, h, "k", string(original), nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/demo/k")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	first := make([]byte, 64<<10)
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatal(err)
	}
	other := bytes.Repeat([]byte("y"), len(original))
	if _, err := store.Put(t.Context(), "demo", "k", bytes.NewReader(other), int64(len(other)), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("demo", "k"); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(resp.Body)
	if err != nil || !bytes.Equal(append(first, rest...), original) {
		t.Fatalf("download after overwrite and delete: %d bytes, %v; want the original", len(first)+len(rest), err)
	}
}
