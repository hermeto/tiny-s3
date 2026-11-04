package s3api_test

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/s3api"
)

var requestIDPattern = regexp.MustCompile(`^[0-9A-F]{16}$`)

type errorBody struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

func writeError(t *testing.T, path string) (*httptest.ResponseRecorder, errorBody) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.URL.Path = path
	w := httptest.NewRecorder()

	s3api.WriteError(w, r, http.StatusNotImplemented, "NotImplemented", "not here")

	body := w.Body.String()
	if !strings.HasPrefix(body, xml.Header) {
		t.Fatalf("body does not start with the XML header: %q", body)
	}
	var e errorBody
	if err := xml.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("unmarshal %q: %v", body, err)
	}
	return w, e
}

func TestWriteError(t *testing.T) {
	t.Parallel()

	w, e := writeError(t, "/bucket/key")

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if got := w.Header().Get("Content-Type"); got != "application/xml" {
		t.Errorf("Content-Type = %q, want application/xml", got)
	}
	if e.Code != "NotImplemented" || e.Message != "not here" || e.Resource != "/bucket/key" {
		t.Errorf("body = %+v", e)
	}
	if !requestIDPattern.MatchString(e.RequestID) {
		t.Errorf("RequestId %q does not match %s", e.RequestID, requestIDPattern)
	}
	if got := w.Header().Get("x-amz-request-id"); got != e.RequestID {
		t.Errorf("x-amz-request-id = %q, body RequestId = %q", got, e.RequestID)
	}
}

func TestWriteErrorUniqueRequestIDs(t *testing.T) {
	t.Parallel()

	_, first := writeError(t, "/")
	_, second := writeError(t, "/")
	if first.RequestID == second.RequestID {
		t.Fatalf("request IDs repeat: %q", first.RequestID)
	}
}

func TestWriteErrorEscapesResource(t *testing.T) {
	t.Parallel()

	const path = `/b/k&<>"`
	_, e := writeError(t, path)
	if e.Resource != path {
		t.Fatalf("Resource = %q, want %q", e.Resource, path)
	}
}
