package s3api

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// fakeBuckets is a BucketStore whose answers the test sets.
type fakeBuckets struct {
	mu        sync.Mutex
	created   []string
	createErr error
	headErr   error
	list      []storage.Bucket
}

func (f *fakeBuckets) ListBuckets() []storage.Bucket { return f.list }

func (f *fakeBuckets) HeadBucket(name string) (storage.Bucket, error) {
	if f.headErr != nil {
		return storage.Bucket{}, f.headErr
	}
	return storage.Bucket{Name: name}, nil
}

func (f *fakeBuckets) CreateBucket(name string) (storage.Bucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return storage.Bucket{}, f.createErr
	}
	f.created = append(f.created, name)
	return storage.Bucket{Name: name, CreationDate: time.Date(2026, 11, 4, 14, 30, 15, 0, time.UTC)}, nil
}

// logBuffer is a JSON logger that tests can read back.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
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

// bucketHandler serves BucketOps over store with a readable JSON log.
func bucketHandler(store BucketStore) (http.Handler, *logBuffer) {
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	return NewHandler(BucketOps(store, log), log), logs
}

func do(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// wantError checks an S3 error response, without a body for HEAD.
func wantError(t *testing.T, w *httptest.ResponseRecorder, method string, e APIError) {
	t.Helper()
	if w.Code != e.Status {
		t.Fatalf("status = %d, want %d; body %q", w.Code, e.Status, w.Body.String())
	}
	if method == http.MethodHead {
		if w.Body.Len() != 0 {
			t.Fatalf("HEAD body = %q, want empty", w.Body.String())
		}
		return
	}
	if got := parseError(t, w.Body.Bytes()); got.Code != e.Code {
		t.Fatalf("code = %q, want %q", got.Code, e.Code)
	}
}

func TestCreateBucketOp(t *testing.T) {
	t.Parallel()
	accepted := map[string]string{
		"no body":          "",
		"whitespace body":  " \r\n\t",
		"no location":      `<CreateBucketConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></CreateBucketConfiguration>`,
		"empty location":   `<CreateBucketConfiguration><LocationConstraint></LocationConstraint></CreateBucketConfiguration>`,
		"us-east-1":        `<CreateBucketConfiguration><LocationConstraint>us-east-1</LocationConstraint></CreateBucketConfiguration>`,
		"64 KiB exactly":   `<CreateBucketConfiguration></CreateBucketConfiguration>` + strings.Repeat(" ", maxCreateBucketBody-len(`<CreateBucketConfiguration></CreateBucketConfiguration>`)),
		"extra elements":   `<CreateBucketConfiguration><Bucket><Type>x</Type></Bucket></CreateBucketConfiguration>`,
		"namespace prefix": `<s3:CreateBucketConfiguration xmlns:s3="http://s3.amazonaws.com/doc/2006-03-01/"><s3:LocationConstraint>us-east-1</s3:LocationConstraint></s3:CreateBucketConfiguration>`,
	}
	for name, body := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &fakeBuckets{}
			h, _ := bucketHandler(store)
			w := do(h, http.MethodPut, "/demo", strings.NewReader(body))
			if w.Code != http.StatusOK || w.Header().Get("Location") != "/demo" || w.Body.Len() != 0 {
				t.Fatalf("got %d, Location %q, body %q", w.Code, w.Header().Get("Location"), w.Body.String())
			}
			if len(store.created) != 1 || store.created[0] != "demo" {
				t.Fatalf("created = %v", store.created)
			}
		})
	}
}

func TestCreateBucketOpRejects(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		target string
		body   io.Reader
		want   APIError
	}{
		"invalid name":       {"/Bad_Name", strings.NewReader(""), ErrInvalidBucketName},
		"invalid name first": {"/ab", strings.NewReader("<oops"), ErrInvalidBucketName},
		"other region":       {"/demo", strings.NewReader(`<CreateBucketConfiguration><LocationConstraint>eu-west-1</LocationConstraint></CreateBucketConfiguration>`), ErrInvalidLocationConstraint},
		"bad xml":            {"/demo", strings.NewReader("<CreateBucketConfiguration>"), ErrMalformedXML},
		"wrong root":         {"/demo", strings.NewReader("<Other></Other>"), ErrMalformedXML},
		"not xml":            {"/demo", strings.NewReader("hello"), ErrMalformedXML},
		"over 64 KiB":        {"/demo", strings.NewReader(strings.Repeat(" ", maxCreateBucketBody+1)), ErrMalformedXML},
		"read error":         {"/demo", iotest.ErrReader(errors.New("boom")), ErrMalformedXML},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &fakeBuckets{}
			h, _ := bucketHandler(store)
			wantError(t, do(h, http.MethodPut, tt.target, tt.body), http.MethodPut, tt.want)
			if len(store.created) != 0 {
				t.Fatalf("created = %v, want none", store.created)
			}
		})
	}
}

func TestCreateBucketOpStoreErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		err    error
		want   APIError
		logged bool
	}{
		"exists":  {storage.ErrBucketExists, ErrBucketAlreadyOwnedByYou, false},
		"invalid": {storage.ErrInvalidBucketName, ErrInvalidBucketName, false},
		"io":      {errors.New("disk on fire"), ErrInternalError, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, logs := bucketHandler(&fakeBuckets{createErr: tt.err})
			w := do(h, http.MethodPut, "/demo", http.NoBody)
			wantError(t, w, http.MethodPut, tt.want)
			recs := logs.records(t)
			if !tt.logged {
				if len(recs) != 0 {
					t.Fatalf("logged %v, want nothing", recs)
				}
				return
			}
			if len(recs) != 1 {
				t.Fatalf("log = %v, want one record", recs)
			}
			rec := recs[0]
			if rec["msg"] != "bucket_error" || rec["level"] != "ERROR" || rec["op"] != "CreateBucket" ||
				rec["bucket"] != "demo" || rec["error"] != "disk on fire" ||
				rec["request_id"] != w.Header().Get("x-amz-request-id") {
				t.Fatalf("log record = %v", rec)
			}
		})
	}
}

func TestHeadBucketOp(t *testing.T) {
	t.Parallel()
	h, _ := bucketHandler(&fakeBuckets{})
	w := do(h, http.MethodHead, "/demo", nil)
	if w.Code != http.StatusOK || w.Header().Get("x-amz-bucket-region") != "us-east-1" || w.Body.Len() != 0 {
		t.Fatalf("got %d, region %q, body %q", w.Code, w.Header().Get("x-amz-bucket-region"), w.Body.String())
	}
}

func TestCheckBucket(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		err    error
		want   APIError
		logged bool
	}{
		"missing": {storage.ErrNoSuchBucket, ErrNoSuchBucket, false},
		"invalid": {storage.ErrInvalidBucketName, ErrInvalidBucketName, false},
		"io":      {errors.New("disk on fire"), ErrInternalError, true},
	}
	for name, tt := range tests {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			t.Run(name+" "+method, func(t *testing.T) {
				t.Parallel()
				h, logs := bucketHandler(&fakeBuckets{headErr: tt.err})
				w := do(h, method, "/demo/key", nil)
				wantError(t, w, method, tt.want)
				recs := logs.records(t)
				if !tt.logged {
					if len(recs) != 0 {
						t.Fatalf("logged %v, want nothing", recs)
					}
					return
				}
				if len(recs) != 1 || recs[0]["msg"] != "bucket_error" || recs[0]["op"] != "CheckBucket" || recs[0]["bucket"] != "demo" {
					t.Fatalf("log = %v", recs)
				}
			})
		}
	}
}

func TestListBucketsOp(t *testing.T) {
	t.Parallel()
	ns := `xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`
	tests := map[string]struct {
		list []storage.Bucket
		want string
	}{
		"empty": {[]storage.Bucket{},
			`<ListAllMyBucketsResult ` + ns + `><Owner><ID>tiny-s3</ID><DisplayName>tiny-s3</DisplayName></Owner><Buckets></Buckets></ListAllMyBucketsResult>`},
		"two": {[]storage.Bucket{
			{Name: "alpha", CreationDate: time.Date(2026, 11, 4, 14, 30, 15, 123456789, time.UTC)},
			{Name: "beta", CreationDate: time.Date(2026, 11, 4, 9, 30, 15, 0, time.FixedZone("EST", -5*3600))},
		}, `<ListAllMyBucketsResult ` + ns + `><Owner><ID>tiny-s3</ID><DisplayName>tiny-s3</DisplayName></Owner><Buckets>` +
			`<Bucket><Name>alpha</Name><CreationDate>2026-11-04T14:30:15.123Z</CreationDate></Bucket>` +
			`<Bucket><Name>beta</Name><CreationDate>2026-11-04T14:30:15.000Z</CreationDate></Bucket>` +
			`</Buckets></ListAllMyBucketsResult>`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, _ := bucketHandler(&fakeBuckets{list: tt.list})
			w := do(h, http.MethodGet, "/?max-buckets=1&prefix=zzz", nil)
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/xml" {
				t.Fatalf("status %d, Content-Type %q", w.Code, w.Header().Get("Content-Type"))
			}
			if got := w.Body.String(); got != xml.Header+tt.want {
				t.Fatalf("body =\n%s\nwant\n%s", got, xml.Header+tt.want)
			}
		})
	}
}
