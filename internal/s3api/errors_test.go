package s3api

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// parseError checks the XML declaration and decodes an S3 error body.
func parseError(t *testing.T, raw []byte) errorBody {
	t.Helper()
	if !strings.HasPrefix(string(raw), xml.Header) {
		t.Fatalf("body does not start with the XML header: %q", raw)
	}
	var e errorBody
	if err := xml.Unmarshal(raw, &e); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return e
}

func TestAPIErrorValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got     APIError
		status  int
		code    string
		message string
	}{
		{ErrNotImplemented, 501, "NotImplemented", "A header or method you provided implies functionality that is not implemented."},
		{ErrMethodNotAllowed, 405, "MethodNotAllowed", "The specified method is not allowed against this resource."},
		{ErrInvalidURI, 400, "InvalidURI", "Couldn't parse the specified URI."},
		{ErrInternalError, 500, "InternalError", "We encountered an internal error. Please try again."},
		{ErrInvalidBucketName, 400, "InvalidBucketName", "The specified bucket is not valid."},
		{ErrNoSuchBucket, 404, "NoSuchBucket", "The specified bucket does not exist."},
		{ErrBucketAlreadyOwnedByYou, 409, "BucketAlreadyOwnedByYou", "Your previous request to create the named bucket succeeded and you already own it."},
		{ErrMalformedXML, 400, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema."},
		{ErrInvalidLocationConstraint, 400, "InvalidLocationConstraint", "The specified location constraint is not valid."},
		{ErrMissingContentLength, 411, "MissingContentLength", "You must provide the Content-Length HTTP header."},
		{ErrInvalidDigest, 400, "InvalidDigest", "The Content-MD5 you specified is not valid."},
		{ErrMetadataTooLarge, 400, "MetadataTooLarge", "Your metadata headers exceed the maximum allowed metadata size."},
		{ErrKeyTooLong, 400, "KeyTooLongError", "Your key is too long."},
		{ErrEntityTooLarge, 400, "EntityTooLarge", "Your proposed upload exceeds the maximum allowed size."},
		{ErrIncompleteBody, 400, "IncompleteBody", "You did not provide the number of bytes specified by the Content-Length HTTP header."},
		{ErrBadDigest, 400, "BadDigest", "The Content-MD5 you specified did not match what we received."},
		{ErrInvalidArgument, 400, "InvalidArgument", "Invalid Argument"},
		{ErrInvalidRequest, 400, "InvalidRequest", "Invalid Request"},
		{ErrBadChecksum, 400, "BadDigest", "The checksum you specified did not match the calculated checksum."},
	}
	for _, tt := range tests {
		if tt.got.Status != tt.status || tt.got.Code != tt.code || tt.got.Message != tt.message {
			t.Errorf("%s = %+v, want {%d %s %q}", tt.code, tt.got, tt.status, tt.code, tt.message)
		}
	}
}

func TestWriteErrorWithoutContextID(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.URL.Path = "/bucket/key"
	w := httptest.NewRecorder()
	WriteError(w, r, ErrNotImplemented)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/xml" {
		t.Errorf("Content-Type = %q, want application/xml", got)
	}
	e := parseError(t, w.Body.Bytes())
	if e.Code != ErrNotImplemented.Code || e.Message != ErrNotImplemented.Message || e.Resource != "/bucket/key" {
		t.Errorf("body = %+v", e)
	}
	if !requestIDPattern.MatchString(e.RequestID) {
		t.Errorf("RequestId %q does not match %s", e.RequestID, requestIDPattern)
	}
	if got := w.Header().Get("x-amz-request-id"); got != e.RequestID {
		t.Errorf("x-amz-request-id = %q, body RequestId = %q", got, e.RequestID)
	}
}

func TestWriteErrorUsesContextID(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodPut, "/b/k", nil)
	r = r.WithContext(withRequestID(r.Context(), "00000000000000AA"))
	w := httptest.NewRecorder()
	WriteError(w, r, ErrMethodNotAllowed)

	e := parseError(t, w.Body.Bytes())
	if e.RequestID != "00000000000000AA" || w.Header().Get("x-amz-request-id") != "00000000000000AA" {
		t.Fatalf("RequestId = %q, header = %q, want 00000000000000AA", e.RequestID, w.Header().Get("x-amz-request-id"))
	}
}

func TestWriteErrorElementOrder(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/b", nil)
	w := httptest.NewRecorder()
	WriteError(w, r, ErrInvalidURI)

	body := w.Body.String()
	order := []string{"<Error>", "<Code>", "<Message>", "<Resource>", "<RequestId>", "</Error>"}
	last := -1
	for _, tag := range order {
		i := strings.Index(body, tag)
		if i <= last {
			t.Fatalf("%s out of order in %q", tag, body)
		}
		last = i
	}
}

func TestWriteErrorEscapesResource(t *testing.T) {
	t.Parallel()

	const path = `/b/k&<>"`
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.URL.Path = path
	w := httptest.NewRecorder()
	WriteError(w, r, ErrNotImplemented)

	if e := parseError(t, w.Body.Bytes()); e.Resource != path {
		t.Fatalf("Resource = %q, want %q", e.Resource, path)
	}
}

func TestWriteErrorHEADHasNoBody(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodHead, "/b/k", nil)
	w := httptest.NewRecorder()
	WriteError(w, r, ErrNotImplemented)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("HEAD body = %q, want empty", w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/xml" {
		t.Errorf("Content-Type = %q, want application/xml", got)
	}
	if !requestIDPattern.MatchString(w.Header().Get("x-amz-request-id")) {
		t.Errorf("x-amz-request-id = %q", w.Header().Get("x-amz-request-id"))
	}
}
