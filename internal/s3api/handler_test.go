package s3api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// call is one invocation of an Op recorded by recordingOps.
type call struct {
	op     string
	target Target
}

type recorder struct {
	mu    sync.Mutex
	calls []call
}

func (rec *recorder) op(name string) Op {
	return func(w http.ResponseWriter, _ *http.Request, t Target) {
		rec.mu.Lock()
		rec.calls = append(rec.calls, call{name, t})
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

func (rec *recorder) taken() []call {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]call(nil), rec.calls...)
}

// recordingOps returns Ops whose every field records its call and answers 204.
func recordingOps() (Ops, *recorder) {
	rec := &recorder{}
	return Ops{
		ListBuckets:   rec.op("ListBuckets"),
		CreateBucket:  rec.op("CreateBucket"),
		HeadBucket:    rec.op("HeadBucket"),
		ListObjectsV2: rec.op("ListObjectsV2"),
		PutObject:     rec.op("PutObject"),
		GetObject:     rec.op("GetObject"),
		HeadObject:    rec.op("HeadObject"),
		DeleteObject:  rec.op("DeleteObject"),
	}, rec
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// serve runs one request through NewHandler with httptest.NewRequest, which
// sets URL.Path the way net/http does.
func serve(h http.Handler, method, target string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method, target string
		header         http.Header
		op             string
		want           Target
	}{
		{"GET", "/", nil, "ListBuckets", Target{}},
		{"GET", "/?x-id=ListBuckets", nil, "ListBuckets", Target{}},
		{"PUT", "/b", nil, "CreateBucket", Target{Bucket: "b"}},
		{"PUT", "/b/", nil, "CreateBucket", Target{Bucket: "b"}},
		{"HEAD", "/b", nil, "HeadBucket", Target{Bucket: "b"}},
		{"GET", "/b?list-type=2", nil, "ListObjectsV2", Target{Bucket: "b"}},
		{"GET", "/b/?list-type=2", nil, "ListObjectsV2", Target{Bucket: "b"}},
		{"GET", "/b?list-type=2&prefix=a&delimiter=/&max-keys=5&continuation-token=t&start-after=s&fetch-owner=true&encoding-type=url", nil, "ListObjectsV2", Target{Bucket: "b"}},
		{"PUT", "/b/k", nil, "PutObject", Target{"b", "k"}},
		{"PUT", "/b/k?x-id=PutObject", nil, "PutObject", Target{"b", "k"}},
		{"GET", "/b/k", nil, "GetObject", Target{"b", "k"}},
		{"GET", "/b/k?response-content-type=text/plain&response-cache-control=no-cache&x-id=GetObject", nil, "GetObject", Target{"b", "k"}},
		{"GET", "/b/k?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc", nil, "GetObject", Target{"b", "k"}},
		{"GET", "/b/k?ACL", nil, "GetObject", Target{"b", "k"}},
		{"GET", "/b?a=%zz&list-type=2", nil, "ListObjectsV2", Target{Bucket: "b"}},
		{"HEAD", "/b/k", nil, "HeadObject", Target{"b", "k"}},
		{"DELETE", "/b/k", nil, "DeleteObject", Target{"b", "k"}},
		{"PUT", "/b/k", http.Header{"Authorization": {"AWS4-HMAC-SHA256 Credential=x"}}, "PutObject", Target{"b", "k"}},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			t.Parallel()
			ops, rec := recordingOps()
			w := serve(NewHandler(ops, discardLogger()), tt.method, tt.target, tt.header)

			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204; body %q", w.Code, w.Body.String())
			}
			calls := rec.taken()
			if len(calls) != 1 || calls[0].op != tt.op || calls[0].target != tt.want {
				t.Fatalf("calls = %+v, want one %s with %+v", calls, tt.op, tt.want)
			}
		})
	}
}

func TestNilOpsNotImplemented(t *testing.T) {
	t.Parallel()

	h := NewHandler(Ops{}, discardLogger())
	for _, rt := range [][2]string{
		{"GET", "/"}, {"PUT", "/b"}, {"HEAD", "/b"}, {"GET", "/b?list-type=2"},
		{"PUT", "/b/k"}, {"GET", "/b/k"}, {"HEAD", "/b/k"}, {"DELETE", "/b/k"},
	} {
		w := serve(h, rt[0], rt[1], nil)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("%s %s: status = %d, want 501", rt[0], rt[1], w.Code)
		}
		if rt[0] != http.MethodHead {
			if e := parseError(t, w.Body.Bytes()); e.Code != "NotImplemented" {
				t.Errorf("%s %s: code = %q", rt[0], rt[1], e.Code)
			}
		}
	}
}

func TestRoutingErrors(t *testing.T) {
	t.Parallel()

	copySource := http.Header{"X-Amz-Copy-Source": {"/src/key"}}
	tests := []struct {
		method, target string
		header         http.Header
		want           APIError
	}{
		// Methods S3 does not support, on every resource kind.
		{"PATCH", "/", nil, ErrMethodNotAllowed},
		{"PATCH", "/b", nil, ErrMethodNotAllowed},
		{"PATCH", "/b/k", nil, ErrMethodNotAllowed},
		{"OPTIONS", "/b/k", nil, ErrMethodNotAllowed},
		{"TRACE", "/b", nil, ErrMethodNotAllowed},
		{"FOO", "/b/k", nil, ErrMethodNotAllowed},
		{"PATCH", "//k", nil, ErrMethodNotAllowed},
		// Supported methods with no meaning on the service.
		{"PUT", "/", nil, ErrMethodNotAllowed},
		{"POST", "/", nil, ErrMethodNotAllowed},
		{"DELETE", "/", nil, ErrMethodNotAllowed},
		{"HEAD", "/", nil, ErrMethodNotAllowed},
		// Real S3 operations outside this server's scope.
		{"GET", "/b", nil, ErrNotImplemented},
		{"GET", "/b?list-type=1", nil, ErrNotImplemented},
		{"GET", "/b?acl", nil, ErrNotImplemented},
		{"GET", "/b?list-type=2&acl", nil, ErrNotImplemented},
		{"GET", "/b?location", nil, ErrNotImplemented},
		{"GET", "/b?versions", nil, ErrNotImplemented},
		{"GET", "/b?uploads", nil, ErrNotImplemented},
		{"PUT", "/b?versioning", nil, ErrNotImplemented},
		{"DELETE", "/b", nil, ErrNotImplemented},
		{"DELETE", "/b?policy", nil, ErrNotImplemented},
		{"POST", "/b", nil, ErrNotImplemented},
		{"POST", "/b?delete", nil, ErrNotImplemented},
		{"PUT", "/b/k?tagging", nil, ErrNotImplemented},
		{"PUT", "/b/k?partNumber=1&uploadId=x", nil, ErrNotImplemented},
		{"GET", "/b/k?uploadId=x", nil, ErrNotImplemented},
		{"GET", "/b/k?versionId=1", nil, ErrNotImplemented},
		{"GET", "/b/k?partNumber=1", nil, ErrNotImplemented},
		{"GET", "/b/k?attributes", nil, ErrNotImplemented},
		{"DELETE", "/b/k?uploadId=x", nil, ErrNotImplemented},
		{"POST", "/b/k", nil, ErrNotImplemented},
		{"POST", "/b/k?uploads", nil, ErrNotImplemented},
		{"POST", "/b/k?restore", nil, ErrNotImplemented},
		{"HEAD", "/b?acl", nil, ErrNotImplemented},
		{"HEAD", "/b/k?versionId=1", nil, ErrNotImplemented},
		{"PUT", "/b/k", copySource, ErrNotImplemented},
		{"GET", "/b?a=%zz&acl", nil, ErrNotImplemented},
		// Malformed paths.
		{"GET", "//", nil, ErrInvalidURI},
		{"GET", "//k", nil, ErrInvalidURI},
		{"PUT", "//k/more", nil, ErrInvalidURI},
		{"HEAD", "//k", nil, ErrInvalidURI},
		{"GET", "/b/%FF", nil, ErrInvalidURI},
		{"GET", "/%FF", nil, ErrInvalidURI},
		{"PUT", "/%C3%28/k", nil, ErrInvalidURI},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			t.Parallel()
			ops, rec := recordingOps()
			w := serve(NewHandler(ops, discardLogger()), tt.method, tt.target, tt.header)

			if calls := rec.taken(); len(calls) != 0 {
				t.Fatalf("operations called: %+v", calls)
			}
			if w.Code != tt.want.Status {
				t.Fatalf("status = %d, want %d", w.Code, tt.want.Status)
			}
			id := w.Header().Get("x-amz-request-id")
			if !requestIDPattern.MatchString(id) {
				t.Fatalf("x-amz-request-id = %q", id)
			}
			if tt.method == http.MethodHead {
				if w.Body.Len() != 0 {
					t.Fatalf("HEAD body = %q, want empty", w.Body.String())
				}
				return
			}
			if got := w.Header().Get("Content-Type"); got != "application/xml" {
				t.Fatalf("Content-Type = %q", got)
			}
			e := parseError(t, w.Body.Bytes())
			if e.Code != tt.want.Code || e.Message != tt.want.Message || e.RequestID != id {
				t.Fatalf("body = %+v, want code %s and RequestId %s", e, tt.want.Code, id)
			}
		})
	}
}

// logRecords decodes JSON log lines.
func logRecords(t *testing.T, buf *syncBuffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// panicServer runs NewHandler on a real server with GetObject set to op and
// ListBuckets answering 204, logging JSON into the returned buffer.
func panicServer(t *testing.T, op Op) (*httptest.Server, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	ops, _ := recordingOps()
	ops.GetObject = op
	srv := httptest.NewServer(NewHandler(ops, slog.New(slog.NewJSONHandler(logs, nil))))
	// net/http logs recovered ErrAbortHandler panics nowhere, but keep its
	// own error log quiet in case it reports the aborted connection.
	srv.Config.ErrorLog = slog.NewLogLogger(slog.DiscardHandler, slog.LevelError)
	t.Cleanup(srv.Close)
	return srv, logs
}

func assertServes(t *testing.T, srv *httptest.Server) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("next request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("next request status = %d, want 204", resp.StatusCode)
	}
}

func TestPanicBeforeWrite(t *testing.T) {
	t.Parallel()

	srv, logs := panicServer(t, func(http.ResponseWriter, *http.Request, Target) { panic("boom") })
	resp, err := srv.Client().Get(srv.URL + "/b/k")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	id := resp.Header.Get("x-amz-request-id")
	e := parseError(t, raw)
	if e.Code != "InternalError" || e.Message != ErrInternalError.Message || e.RequestID != id || e.Resource != "/b/k" {
		t.Fatalf("body = %+v, header id %q", e, id)
	}
	recs := logRecords(t, logs)
	if len(recs) != 1 {
		t.Fatalf("log records = %v, want 1", recs)
	}
	rec := recs[0]
	if rec["msg"] != "handler_panic" || rec["level"] != "ERROR" || rec["request_id"] != id ||
		rec["method"] != "GET" || rec["path"] != "/b/k" || rec["panic"] != "boom" {
		t.Fatalf("log record = %v", rec)
	}
	assertServes(t, srv)
}

func TestPanicAfterWriteAborts(t *testing.T) {
	t.Parallel()

	tests := map[string]Op{
		"after WriteHeader": func(w http.ResponseWriter, _ *http.Request, _ Target) {
			w.WriteHeader(http.StatusOK)
			panic("late")
		},
		"after Write": func(w http.ResponseWriter, _ *http.Request, _ Target) {
			_, _ = io.WriteString(w, "partial")
			panic("late")
		},
	}
	for name, op := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, logs := panicServer(t, op)
			resp, err := srv.Client().Get(srv.URL + "/b/k")
			if err == nil {
				// Headers may arrive before the abort; the body must not complete.
				_, err = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
			if err == nil {
				t.Fatal("response completed, want an aborted connection")
			}
			recs := logRecords(t, logs)
			if len(recs) != 1 || recs[0]["msg"] != "handler_panic" || recs[0]["panic"] != "late" {
				t.Fatalf("log records = %v", recs)
			}
			assertServes(t, srv)
		})
	}
}

func TestPanicErrAbortHandler(t *testing.T) {
	t.Parallel()

	srv, logs := panicServer(t, func(http.ResponseWriter, *http.Request, Target) { panic(http.ErrAbortHandler) })
	if resp, err := srv.Client().Get(srv.URL + "/b/k"); err == nil {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want an aborted connection", resp.StatusCode)
	}
	if s := logs.String(); s != "" {
		t.Fatalf("logged %q, want nothing", s)
	}
	assertServes(t, srv)
}

func TestWrapperUnwrap(t *testing.T) {
	t.Parallel()

	srv, _ := panicServer(t, func(w http.ResponseWriter, _ *http.Request, _ Target) {
		_, _ = io.WriteString(w, "chunk")
		if err := http.NewResponseController(w).Flush(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	resp, err := srv.Client().Get(srv.URL + "/b/k")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(raw) != "chunk" || resp.TransferEncoding == nil {
		t.Fatalf("body = %q, transfer-encoding = %v; want a flushed chunked response", raw, resp.TransferEncoding)
	}
}

func TestRequestIDOnSuccess(t *testing.T) {
	t.Parallel()

	var seen string
	ops, _ := recordingOps()
	ops.GetObject = func(w http.ResponseWriter, r *http.Request, _ Target) {
		seen = RequestID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}
	w := serve(NewHandler(ops, discardLogger()), "GET", "/b/k", nil)

	id := w.Header().Get("x-amz-request-id")
	if !requestIDPattern.MatchString(id) || seen != id {
		t.Fatalf("header = %q, RequestID(ctx) = %q", id, seen)
	}
}

func TestRequestIDSharedWithOpError(t *testing.T) {
	t.Parallel()

	ops, _ := recordingOps()
	ops.HeadObject = func(w http.ResponseWriter, r *http.Request, _ Target) { WriteError(w, r, ErrNotImplemented) }
	ops.GetObject = func(w http.ResponseWriter, r *http.Request, _ Target) { WriteError(w, r, ErrNotImplemented) }
	h := NewHandler(ops, discardLogger())

	w := serve(h, "GET", "/b/k", nil)
	if e := parseError(t, w.Body.Bytes()); e.RequestID != w.Header().Get("x-amz-request-id") {
		t.Fatalf("RequestId = %q, header = %q", e.RequestID, w.Header().Get("x-amz-request-id"))
	}
	if w := serve(h, "HEAD", "/b/k", nil); w.Body.Len() != 0 || w.Header().Get("x-amz-request-id") == "" {
		t.Fatalf("HEAD: body %q, header %q", w.Body.String(), w.Header().Get("x-amz-request-id"))
	}
}

func TestRequestIDUnique(t *testing.T) {
	t.Parallel()

	ops, _ := recordingOps()
	h := NewHandler(ops, discardLogger())
	requests := [][2]string{
		{"GET", "/b/k"},    // success
		{"GET", "/b?acl"},  // 501
		{"PATCH", "/b"},    // 405
		{"GET", "//k"},     // 400
		{"HEAD", "/b?acl"}, // 501 without body
		{"HEAD", "/b/k"},   // success without body
	}
	seen := make(map[string]bool)
	for i := range 1000 {
		rq := requests[i%len(requests)]
		id := serve(h, rq[0], rq[1], nil).Header().Get("x-amz-request-id")
		if !requestIDPattern.MatchString(id) {
			t.Fatalf("%s %s: x-amz-request-id = %q", rq[0], rq[1], id)
		}
		if seen[id] {
			t.Fatalf("request ID %q repeated after %d requests", id, i)
		}
		seen[id] = true
	}
}

// gatedOps returns recordingOps whose CheckBucket lets only bucket "b"
// through and answers NoSuchBucket otherwise.
func gatedOps() (Ops, *recorder, *[]string) {
	ops, rec := recordingOps()
	var checked []string
	var mu sync.Mutex
	ops.CheckBucket = func(_ *http.Request, bucket string) (APIError, bool) {
		mu.Lock()
		checked = append(checked, bucket)
		mu.Unlock()
		if bucket == "b" {
			return APIError{}, true
		}
		return ErrNoSuchBucket, false
	}
	return ops, rec, &checked
}

func TestBucketGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method, target string
		op             string // "" when the gate answers
		checked        bool
	}{
		{"HEAD", "/b", "HeadBucket", true},
		{"GET", "/b?list-type=2", "ListObjectsV2", true},
		{"PUT", "/b/k", "PutObject", true},
		{"GET", "/b/k", "GetObject", true},
		{"HEAD", "/b/k", "HeadObject", true},
		{"DELETE", "/b/k", "DeleteObject", true},
		{"HEAD", "/missing", "", true},
		{"GET", "/missing?list-type=2", "", true},
		{"PUT", "/missing/k", "", true},
		{"GET", "/missing/k", "", true},
		{"HEAD", "/missing/k", "", true},
		{"DELETE", "/missing/k", "", true},
		{"PUT", "/missing", "CreateBucket", false},
		{"GET", "/", "ListBuckets", false},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			t.Parallel()
			ops, rec, checked := gatedOps()
			w := serve(NewHandler(ops, discardLogger()), tt.method, tt.target, nil)
			if got := len(*checked) > 0; got != tt.checked {
				t.Fatalf("checked = %v, want %v", *checked, tt.checked)
			}
			calls := rec.taken()
			if tt.op == "" {
				if len(calls) != 0 {
					t.Fatalf("ops called %v, want none", calls)
				}
				if w.Code != http.StatusNotFound {
					t.Fatalf("status = %d, want 404", w.Code)
				}
				if tt.method == "HEAD" && w.Body.Len() != 0 {
					t.Fatalf("HEAD body = %q, want empty", w.Body.String())
				}
				if tt.method != "HEAD" && parseError(t, w.Body.Bytes()).Code != "NoSuchBucket" {
					t.Fatalf("body = %q, want NoSuchBucket", w.Body.String())
				}
				return
			}
			if len(calls) != 1 || calls[0].op != tt.op {
				t.Fatalf("ops called %v, want %s", calls, tt.op)
			}
		})
	}
}

func TestBucketGateSkipsRoutingErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		method, target string
		status         int
	}{
		{"GET", "/missing?acl", http.StatusNotImplemented},
		{"PATCH", "/missing/k", http.StatusMethodNotAllowed},
		{"POST", "/missing/k", http.StatusNotImplemented},
	} {
		ops, _, checked := gatedOps()
		w := serve(NewHandler(ops, discardLogger()), tt.method, tt.target, nil)
		if w.Code != tt.status || len(*checked) != 0 {
			t.Fatalf("%s %s: status %d, checked %v; want %d unchecked", tt.method, tt.target, w.Code, *checked, tt.status)
		}
	}
}

func TestBucketGateThenNotImplemented(t *testing.T) {
	t.Parallel()
	ops, _, checked := gatedOps()
	ops.GetObject = nil
	w := serve(NewHandler(ops, discardLogger()), "GET", "/b/k", nil)
	if w.Code != http.StatusNotImplemented || len(*checked) != 1 {
		t.Fatalf("status %d, checked %v; want 501 after one check", w.Code, *checked)
	}
	w = serve(NewHandler(ops, discardLogger()), "GET", "/missing/k", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing bucket: status %d, want 404", w.Code)
	}
}
