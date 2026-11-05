package s3api

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// rangeObject stores 1000 random bytes as demo/r and returns the handler and
// the bytes.
func rangeObject(t *testing.T) (http.Handler, []byte) {
	t.Helper()
	h, _, _, _ := readStore(t, nil)
	body := make([]byte, 1000)
	_, _ = rand.Read(body)
	mustPut(t, h, "r", string(body), nil)
	return h, body
}

func rangeReq(method, rng string, header http.Header) *http.Request {
	r := httptest.NewRequest(method, "/demo/r", nil)
	r.Header.Set("Range", rng)
	for k, v := range header {
		r.Header[k] = v
	}
	return r
}

func TestRangeServed(t *testing.T) {
	t.Parallel()
	h, body := rangeObject(t)
	full := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/r", nil))
	for _, tt := range []struct {
		rng         string
		first, last int
	}{
		{"bytes=0-99", 0, 99},
		{"bytes=100-", 100, 999},
		{"bytes=-50", 950, 999},
		{"bytes=990-5000", 990, 999},
		{"bytes=-5000", 0, 999},
		{"bytes=0-", 0, 999},
	} {
		w := serveReq(h, rangeReq(http.MethodGet, tt.rng, nil))
		want := body[tt.first : tt.last+1]
		if w.Code != http.StatusPartialContent || w.Body.String() != string(want) {
			t.Fatalf("%s: %d, %d bytes; want 206 with %d bytes", tt.rng, w.Code, w.Body.Len(), len(want))
		}
		if got := w.Header().Get("Content-Range"); got != fmt.Sprintf("bytes %d-%d/1000", tt.first, tt.last) {
			t.Fatalf("%s: Content-Range %q", tt.rng, got)
		}
		if got := w.Header().Get("Content-Length"); got != fmt.Sprint(len(want)) {
			t.Fatalf("%s: Content-Length %q", tt.rng, got)
		}
		for _, k := range []string{"ETag", "Content-Type", "Last-Modified", "Accept-Ranges"} {
			if w.Header().Get(k) != full.Header().Get(k) {
				t.Fatalf("%s: %s = %q, full GET has %q", tt.rng, k, w.Header().Get(k), full.Header().Get(k))
			}
		}

		head := serveReq(h, rangeReq(http.MethodHead, tt.rng, nil))
		if head.Code != http.StatusPartialContent || head.Body.Len() != 0 ||
			head.Header().Get("Content-Range") != w.Header().Get("Content-Range") ||
			head.Header().Get("Content-Length") != w.Header().Get("Content-Length") {
			t.Fatalf("%s: HEAD %d %v", tt.rng, head.Code, head.Header())
		}
	}
}

// TestRangeReadFailureAborts fails a read inside a range after the status.
func TestRangeReadFailureAborts(t *testing.T) {
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
	log := slog.New(slog.DiscardHandler)
	srv := httptest.NewServer(NewHandler(readOps(store, log), log))
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/demo/k", nil)
	req.Header.Set("Range", "bytes=1000-")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || err == nil || len(got) >= len(body)-1000 {
		t.Fatalf("status %d, %d bytes, err %v; want 206 then a failed read", resp.StatusCode, len(got), err)
	}
}
