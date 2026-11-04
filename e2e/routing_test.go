//go:build e2e

package e2e

import (
	"encoding/xml"
	"io"
	"net/http"
	"regexp"
	"syscall"
	"testing"
	"time"
)

var requestIDPattern = regexp.MustCompile(`^[0-9A-F]{16}$`)

// TestRouting checks that the built binary serves the S3 router: S3 XML
// errors, no body on HEAD, and a request ID on every response.
func TestRouting(t *testing.T) {
	p := startProc(t)
	base := "http://" + p.addr

	tests := []struct {
		method, path string
		status       int
		code         string // "" for HEAD
	}{
		{http.MethodGet, "/", http.StatusNotImplemented, "NotImplemented"},
		{http.MethodPatch, "/b/k", http.StatusMethodNotAllowed, "MethodNotAllowed"},
		{http.MethodGet, "//k", http.StatusBadRequest, "InvalidURI"},
		{http.MethodHead, "/b/k", http.StatusNotImplemented, ""},
	}
	for _, tt := range tests {
		req, err := http.NewRequest(tt.method, base+tt.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}

		id := resp.Header.Get("x-amz-request-id")
		if resp.StatusCode != tt.status || !requestIDPattern.MatchString(id) {
			t.Fatalf("%s %s: status %d, request id %q", tt.method, tt.path, resp.StatusCode, id)
		}
		if tt.code == "" {
			if len(raw) != 0 {
				t.Fatalf("%s %s: body %q, want empty", tt.method, tt.path, raw)
			}
			continue
		}
		var e struct {
			Code      string `xml:"Code"`
			RequestID string `xml:"RequestId"`
		}
		if err := xml.Unmarshal(raw, &e); err != nil {
			t.Fatalf("%s %s: unmarshal %q: %v", tt.method, tt.path, raw, err)
		}
		if e.Code != tt.code || e.RequestID != id {
			t.Fatalf("%s %s: body %+v, header id %q", tt.method, tt.path, e, id)
		}
	}

	p.signal(t, syscall.SIGTERM)
	if code, _ := p.exit(t, time.Now()); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}
