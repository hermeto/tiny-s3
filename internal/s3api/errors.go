// Package s3api is the S3 protocol layer: request routing and S3 XML error
// responses.
package s3api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
)

// Error is the S3 XML error body.
type Error struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

// WriteError writes an S3 XML error response. The same request ID is sent in
// the x-amz-request-id header and the RequestId element.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	id := newRequestID()
	h := w.Header()
	h.Set("Content-Type", "application/xml")
	h.Set("x-amz-request-id", id)
	w.WriteHeader(status)

	// Headers are already sent, so a write or encode error here means the
	// client went away; there is nothing useful left to report to it.
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(Error{
		Code:      code,
		Message:   message,
		Resource:  r.URL.Path,
		RequestID: id,
	})
}

// newRequestID returns 16 uppercase hex characters. crypto/rand.Read never
// returns an error (Go 1.24+).
func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return strings.ToUpper(hex.EncodeToString(b[:]))
}
