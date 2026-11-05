package s3api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// putCall is one Put the fake store received.
type putCall struct {
	bucket, key string
	size        int64
	body        []byte
	opts        storage.PutOptions
}

// fakeObjects is an ObjectStore that records Puts and reads their bodies.
type fakeObjects struct {
	mu    sync.Mutex
	calls []putCall
	err   error
}

func (f *fakeObjects) Put(_ context.Context, bucket, key string, body io.Reader, size int64, opts storage.PutOptions) (storage.Info, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return storage.Info{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, putCall{bucket, key, size, b, opts})
	if f.err != nil {
		return storage.Info{}, f.err
	}
	sum := md5.Sum(b)
	return storage.Info{Bucket: bucket, Key: key, Size: size, MD5: hex.EncodeToString(sum[:])}, nil
}

// Get and Head are not used by the PutObject tests; reads go through a real
// store.
func (f *fakeObjects) Get(string, string) (storage.Info, storage.Reader, error) {
	return storage.Info{}, nil, storage.ErrNoSuchKey
}

func (f *fakeObjects) Head(string, string) (storage.Info, error) {
	return storage.Info{}, storage.ErrNoSuchKey
}

func (f *fakeObjects) Delete(string, string) error { return nil }

func (f *fakeObjects) taken() []putCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]putCall(nil), f.calls...)
}

// objectHandler serves PutObjectOp over store with a readable log and no
// bucket gate.
func objectHandler(store ObjectStore) (http.Handler, *logBuffer) {
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	return NewHandler(Ops{PutObject: PutObjectOp(store, log)}, log), logs
}

// putReq builds a PUT with body and its Content-Length.
func putReq(target, body string, header http.Header) *http.Request {
	r := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	for k, v := range header {
		r.Header[k] = v
	}
	return r
}

func serveReq(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPutObject(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ target, key, body string }{
		"simple":     {"/demo/a/b.txt", "a/b.txt", "hello world"},
		"empty":      {"/demo/empty", "empty", ""},
		"spaces":     {"/demo/a%20b", "a b", "x"},
		"plus":       {"/demo/a+b", "a+b", "x"},
		"utf-8":      {"/demo/%C3%A1/%E2%82%AC", "á/€", "x"},
		"slash key":  {"/demo/dir/", "dir/", "x"},
		"percent 2F": {"/demo/a%2Fb", "a/b", "x"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &fakeObjects{}
			h, _ := objectHandler(store)
			w := serveReq(h, putReq(tt.target, tt.body, nil))
			sum := md5.Sum([]byte(tt.body))
			if want := `"` + hex.EncodeToString(sum[:]) + `"`; w.Code != http.StatusOK || w.Header().Get("ETag") != want || w.Body.Len() != 0 {
				t.Fatalf("got %d, ETag %q, body %q; want 200 %s", w.Code, w.Header().Get("ETag"), w.Body.String(), want)
			}
			for k := range w.Header() {
				if k != "Etag" && k != "X-Amz-Request-Id" {
					t.Fatalf("unexpected response header %s", k)
				}
			}
			calls := store.taken()
			if len(calls) != 1 {
				t.Fatalf("calls = %v", calls)
			}
			c := calls[0]
			if c.bucket != "demo" || c.key != tt.key || c.size != int64(len(tt.body)) || !bytes.Equal(c.body, []byte(tt.body)) {
				t.Fatalf("call = %+v", c)
			}
		})
	}
}

func TestPutObjectContentTypeAndMetadata(t *testing.T) {
	t.Parallel()
	store := &fakeObjects{}
	h, _ := objectHandler(store)
	r := putReq("/demo/k", "x", http.Header{
		"Content-Type":     {"text/plain"},
		"X-Amz-Meta-Owner": {"alice"},
		"X-Amz-Meta-Team":  {"a", "b"},
		"x-amz-meta-MiXeD": {"v"},
		"X-Amz-Metadata":   {"not user metadata"},
	})
	if w := serveReq(h, r); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	opts := store.taken()[0].opts
	want := map[string]string{"owner": "alice", "team": "a,b", "mixed": "v"}
	if opts.ContentType != "text/plain" || len(opts.Metadata) != len(want) {
		t.Fatalf("opts = %+v", opts)
	}
	for k, v := range want {
		if opts.Metadata[k] != v {
			t.Fatalf("metadata = %v, want %v", opts.Metadata, want)
		}
	}

	// No content type and no metadata: the store applies its default.
	store = &fakeObjects{}
	h, _ = objectHandler(store)
	serveReq(h, putReq("/demo/k", "x", nil))
	if opts := store.taken()[0].opts; opts.ContentType != "" || opts.Metadata != nil {
		t.Fatalf("opts = %+v, want empty", opts)
	}
}

func TestPutObjectMetadataLimit(t *testing.T) {
	t.Parallel()
	// "k" plus a value: 1 + 2047 bytes is exactly the limit.
	for _, tt := range []struct {
		value string
		ok    bool
	}{
		{strings.Repeat("v", 2047), true},
		{strings.Repeat("v", 2048), false},
	} {
		store := &fakeObjects{}
		h, _ := objectHandler(store)
		w := serveReq(h, putReq("/demo/k", "x", http.Header{"X-Amz-Meta-K": {tt.value}}))
		if tt.ok {
			if w.Code != http.StatusOK {
				t.Fatalf("2048 bytes: status %d", w.Code)
			}
			continue
		}
		wantError(t, w, http.MethodPut, ErrMetadataTooLarge)
		if calls := store.taken(); len(calls) != 0 {
			t.Fatalf("store called on oversized metadata: %v", calls)
		}
	}
}

// unreadBody fails the test if the handler reads the body.
type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) {
	b.t.Error("body was read")
	return 0, io.EOF
}

func TestPutObjectRejectsBeforeReadingBody(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		header http.Header
		length int64
		want   APIError
	}{
		"framed without decoded length":    {http.Header{"Content-Encoding": {"aws-chunked"}}, 10, ErrMissingContentLength},
		"streaming without decoded length": {http.Header{"X-Amz-Content-Sha256": {"STREAMING-UNSIGNED-PAYLOAD-TRAILER"}}, -1, ErrMissingContentLength},
		"decoded length not a number":      {http.Header{"Content-Encoding": {"aws-chunked"}, "X-Amz-Decoded-Content-Length": {"ten"}}, 10, ErrInvalidArgument},
		"decoded length negative":          {http.Header{"Content-Encoding": {"aws-chunked"}, "X-Amz-Decoded-Content-Length": {"-1"}}, 10, ErrInvalidArgument},
		"two checksums":                    {http.Header{"X-Amz-Checksum-Crc32": {"DUoRhQ=="}, "X-Amz-Checksum-Sha1": {"Kq5sNclPz7QV2+lfQIuc6R7oRu0="}}, 10, ErrInvalidRequest},
		"bad checksum header":              {http.Header{"X-Amz-Checksum-Crc32": {"nope"}}, 10, ErrInvalidRequest},
		"no length":                        {nil, -1, ErrMissingContentLength},
		"md5 not base64":                   {http.Header{"Content-Md5": {"not base64!"}}, 10, ErrInvalidDigest},
		"md5 wrong length":                 {http.Header{"Content-Md5": {"AAAA"}}, 10, ErrInvalidDigest},
		"metadata too large":               {http.Header{"X-Amz-Meta-K": {strings.Repeat("v", 2048)}}, 10, ErrMetadataTooLarge},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &fakeObjects{}
			h, _ := objectHandler(store)
			r := httptest.NewRequest(http.MethodPut, "/demo/k", unreadBody{t})
			r.ContentLength = tt.length
			for k, v := range tt.header {
				r.Header[k] = v
			}
			wantError(t, serveReq(h, r), http.MethodPut, tt.want)
			if calls := store.taken(); len(calls) != 0 {
				t.Fatalf("store called: %v", calls)
			}
		})
	}
}

func TestPutObjectContentMD5(t *testing.T) {
	t.Parallel()
	store := &fakeObjects{}
	h, _ := objectHandler(store)
	sum := md5.Sum([]byte("x"))
	r := putReq("/demo/k", "x", http.Header{"Content-Md5": {base64.StdEncoding.EncodeToString(sum[:])}})
	if w := serveReq(h, r); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if got := store.taken()[0].opts.ContentMD5; !bytes.Equal(got, sum[:]) {
		t.Fatalf("ContentMD5 = %x, want %x", got, sum)
	}
}

func TestPutObjectStoreErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		err  error
		want APIError
	}{
		{storage.ErrBadDigest, ErrBadDigest},
		{storage.ErrIncompleteBody, ErrIncompleteBody},
		{storage.ErrKeyTooLong, ErrKeyTooLong},
		{storage.ErrEntityTooLarge, ErrEntityTooLarge},
		{errors.New("disk on fire"), ErrInternalError},
	} {
		h, _ := objectHandler(&fakeObjects{err: tt.err})
		wantError(t, serveReq(h, putReq("/demo/k", "x", nil)), http.MethodPut, tt.want)
	}
}

// realStoreServer serves PutObjectOp over a real store on a real listener.
func realStoreServer(t *testing.T) (*httptest.Server, *storage.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.Open(fsx.OS{}, root)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler(Ops{PutObject: PutObjectOp(store, discardLogger())}, discardLogger()))
	t.Cleanup(srv.Close)
	return srv, store, root
}

func TestPutObjectEarlyDisconnect(t *testing.T) {
	t.Parallel()
	srv, store, root := realStoreServer(t)
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "PUT /demo/k HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\n0123456789"); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || parseError(t, raw).Code != "IncompleteBody" {
		t.Fatalf("got %d %q, want 400 IncompleteBody", resp.StatusCode, raw)
	}
	if _, err := store.Head("demo", "k"); !errors.Is(err, storage.ErrNoSuchKey) {
		t.Fatalf("Head = %v, want ErrNoSuchKey", err)
	}
	for _, dir := range []string{"tmp", "data", "meta"} {
		des, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil || len(des) != 0 {
			t.Fatalf("%s = %v (%v), want empty", dir, des, err)
		}
	}
}

func TestPutObjectLimitsBeforeReadingBody(t *testing.T) {
	t.Parallel()
	_, store, _ := realStoreServer(t)
	h := NewHandler(Ops{PutObject: PutObjectOp(store, discardLogger())}, discardLogger())
	for _, tt := range []struct {
		key    string
		length int64
		want   APIError
	}{
		{"k", storage.MaxObjectSize + 1, ErrEntityTooLarge},
		{strings.Repeat("k", storage.MaxKeyLength+1), 1, ErrKeyTooLong},
	} {
		r := httptest.NewRequest(http.MethodPut, "/demo/"+tt.key, unreadBody{t})
		r.ContentLength = tt.length
		wantError(t, serveReq(h, r), http.MethodPut, tt.want)
	}
}

const cliStream = "b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:jSnVw/bqjr4=\r\n\r\n"

// framedReq builds an aws-chunked PUT of stream as an HTTP chunked request
// (ContentLength -1), the way the aws CLI sends it over HTTPS.
func framedReq(target, stream string, header http.Header) *http.Request {
	r := httptest.NewRequest(http.MethodPut, target, strings.NewReader(stream))
	r.ContentLength = -1
	r.Header.Set("Content-Encoding", "aws-chunked")
	r.Header.Set("X-Amz-Content-Sha256", "STREAMING-UNSIGNED-PAYLOAD-TRAILER")
	r.Header.Set("X-Amz-Decoded-Content-Length", "11")
	r.Header.Set("X-Amz-Trailer", "x-amz-checksum-crc64nvme")
	for k, v := range header {
		r.Header[k] = v
	}
	return r
}

func TestPutObjectFramed(t *testing.T) {
	t.Parallel()
	sig := ";chunk-signature=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := map[string]struct {
		stream string
		header http.Header
		length int64
	}{
		"unsigned trailer":              {cliStream, nil, -1},
		"content length of the framing": {cliStream, nil, int64(len(cliStream))},
		"gzip too":                      {cliStream, http.Header{"Content-Encoding": {"aws-chunked,gzip"}}, -1},
		"only the streaming hash":       {cliStream, http.Header{"Content-Encoding": nil}, -1},
		"signed": {"5" + sig + "\r\nhello\r\n6" + sig + "\r\n world\r\n0" + sig + "\r\n\r\n",
			http.Header{"X-Amz-Content-Sha256": {"STREAMING-AWS4-HMAC-SHA256-PAYLOAD"}, "X-Amz-Trailer": nil}, -1},
		"signed trailer": {"b" + sig + "\r\nhello world\r\n0" + sig + "\r\nx-amz-checksum-crc32:DUoRhQ==\r\nx-amz-trailer-signature:00\r\n\r\n",
			http.Header{"X-Amz-Content-Sha256": {"STREAMING-AWS4-HMAC-SHA256-PAYLOAD-TRAILER"}, "X-Amz-Trailer": {"x-amz-checksum-crc32"}}, -1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, store, _ := realStoreServer(t)
			_ = srv
			h := NewHandler(Ops{PutObject: PutObjectOp(store, discardLogger())}, discardLogger())
			r := framedReq("/demo/k", tt.stream, tt.header)
			r.ContentLength = tt.length
			w := serveReq(h, r)
			if w.Code != http.StatusOK || w.Header().Get("ETag") != `"5eb63bbbe01eeed093cb22bb8f5acdc3"` {
				t.Fatalf("got %d %q, ETag %q", w.Code, w.Body.String(), w.Header().Get("ETag"))
			}
			info, rd, err := store.Get("demo", "k")
			if err != nil {
				t.Fatal(err)
			}
			defer rd.Close()
			got, _ := io.ReadAll(rd)
			if string(got) != "hello world" || info.Size != 11 {
				t.Fatalf("stored %q (size %d)", got, info.Size)
			}
		})
	}
}

func TestPutObjectPlainChecksumHeader(t *testing.T) {
	t.Parallel()
	store := &fakeObjects{}
	h, _ := objectHandler(store)
	w := serveReq(h, putReq("/demo/k", "hello world", http.Header{"X-Amz-Checksum-Crc64nvme": {"jSnVw/bqjr4="}}))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if c := store.taken()[0]; string(c.body) != "hello world" || c.opts.Verify == nil || c.opts.Verify() != nil {
		t.Fatalf("call = %+v", c)
	}
}

// realHandler serves PutObjectOp over a fresh real store.
func realHandler(t *testing.T) (http.Handler, *storage.Store, string) {
	t.Helper()
	_, store, root := realStoreServer(t)
	return NewHandler(Ops{PutObject: PutObjectOp(store, discardLogger())}, discardLogger()), store, root
}

// assertUnchanged checks that demo/k still holds "previous" and that no
// temp file is left.
func assertUnchanged(t *testing.T, store *storage.Store, root string) {
	t.Helper()
	_, rd, err := store.Get("demo", "k")
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	if got, _ := io.ReadAll(rd); string(got) != "previous" {
		t.Fatalf("object = %q, want previous", got)
	}
	if des, err := os.ReadDir(filepath.Join(root, "tmp")); err != nil || len(des) != 0 {
		t.Fatalf("tmp = %v (%v), want empty", des, err)
	}
}

func seedPrevious(t *testing.T, h http.Handler) {
	t.Helper()
	if w := serveReq(h, putReq("/demo/k", "previous", nil)); w.Code != http.StatusOK {
		t.Fatalf("seed: %d", w.Code)
	}
}

func TestPutObjectChecksumMismatch(t *testing.T) {
	t.Parallel()
	wrong := map[string]string{
		"x-amz-checksum-crc32":     "AAAAAA==",
		"x-amz-checksum-crc32c":    "AAAAAA==",
		"x-amz-checksum-crc64nvme": "AAAAAAAAAAA=",
		"x-amz-checksum-sha1":      base64.StdEncoding.EncodeToString(make([]byte, 20)),
		"x-amz-checksum-sha256":    base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
	for name, value := range wrong {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, store, root := realHandler(t)
			seedPrevious(t, h)
			stream := "b\r\nhello world\r\n0\r\n" + name + ":" + value + "\r\n\r\n"
			w := serveReq(h, framedReq("/demo/k", stream, http.Header{"X-Amz-Trailer": {name}}))
			wantError(t, w, http.MethodPut, ErrBadChecksum)
			if msg := parseError(t, w.Body.Bytes()).Message; msg != ErrBadChecksum.Message {
				t.Fatalf("message %q", msg)
			}
			assertUnchanged(t, store, root)

			// The same value as a header on a plain upload.
			wantError(t, serveReq(h, putReq("/demo/k", "hello world", http.Header{http.CanonicalHeaderKey(name): {value}})), http.MethodPut, ErrBadDigest)
			assertUnchanged(t, store, root)
		})
	}
}

func TestPutObjectInvalidTrailer(t *testing.T) {
	t.Parallel()
	tests := map[string]*http.Request{
		"bad trailer value":       framedReq("/demo/k", "b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:AAAA\r\n\r\n", nil),
		"trailer never arrives":   framedReq("/demo/k", "b\r\nhello world\r\n0\r\n\r\n", nil),
		"trailer on a plain body": putReq("/demo/k", "hello world", http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc32"}}),
	}
	for name, r := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, store, root := realHandler(t)
			seedPrevious(t, h)
			wantError(t, serveReq(h, r), http.MethodPut, ErrInvalidRequest)
			assertUnchanged(t, store, root)
		})
	}
}

func TestPutObjectMalformedStreams(t *testing.T) {
	t.Parallel()
	noTrailer := http.Header{"X-Amz-Trailer": nil}
	tests := map[string]struct {
		stream string
		header http.Header
		want   APIError
	}{
		"truncated":      {"b\r\nhello wo", noTrailer, ErrIncompleteBody},
		"no final line":  {"b\r\nhello world\r\n0\r\n", noTrailer, ErrIncompleteBody},
		"bad chunk size": {"zz\r\nhello world\r\n0\r\n\r\n", noTrailer, ErrInvalidRequest},
		"long line":      {strings.Repeat("0", 4100) + "b\r\nhello world\r\n0\r\n\r\n", noTrailer, ErrInvalidRequest},
		"fewer bytes":    {"5\r\nhello\r\n0\r\n\r\n", noTrailer, ErrIncompleteBody},
		"more bytes":     {"c\r\nhello world!\r\n0\r\n\r\n", noTrailer, ErrIncompleteBody},
		"missing crlf":   {"b\r\nhello worldXX0\r\n\r\n", noTrailer, ErrInvalidRequest},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, store, root := realHandler(t)
			seedPrevious(t, h)
			wantError(t, serveReq(h, framedReq("/demo/k", tt.stream, tt.header)), http.MethodPut, tt.want)
			assertUnchanged(t, store, root)
		})
	}
}

func TestPutObjectFramedLimitsBeforeReadingBody(t *testing.T) {
	t.Parallel()
	h, _, _ := realHandler(t)
	r := httptest.NewRequest(http.MethodPut, "/demo/k", unreadBody{t})
	r.ContentLength = -1
	r.Header.Set("Content-Encoding", "aws-chunked")
	r.Header.Set("X-Amz-Decoded-Content-Length", strconv.FormatInt(storage.MaxObjectSize+1, 10))
	wantError(t, serveReq(h, r), http.MethodPut, ErrEntityTooLarge)
}
