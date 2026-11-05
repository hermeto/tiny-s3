// Package s3api is the S3 protocol layer: path-style request routing, request
// IDs and S3 XML error responses.
package s3api

import (
	"encoding/xml"
	"io"
	"net/http"
)

// APIError is an S3 error: the HTTP status and the Code and Message elements
// of the XML body.
type APIError struct {
	Status  int
	Code    string
	Message string
}

// Errors produced by the router. Messages are the ones S3 sends.
var (
	ErrNotImplemented = APIError{http.StatusNotImplemented, "NotImplemented",
		"A header or method you provided implies functionality that is not implemented."}
	ErrMethodNotAllowed = APIError{http.StatusMethodNotAllowed, "MethodNotAllowed",
		"The specified method is not allowed against this resource."}
	ErrInvalidURI = APIError{http.StatusBadRequest, "InvalidURI",
		"Couldn't parse the specified URI."}
	ErrInternalError = APIError{http.StatusInternalServerError, "InternalError",
		"We encountered an internal error. Please try again."}
)

// Errors produced by the bucket operations and the bucket gate.
var (
	ErrInvalidBucketName = APIError{http.StatusBadRequest, "InvalidBucketName",
		"The specified bucket is not valid."}
	ErrNoSuchBucket = APIError{http.StatusNotFound, "NoSuchBucket",
		"The specified bucket does not exist."}
	ErrBucketAlreadyOwnedByYou = APIError{http.StatusConflict, "BucketAlreadyOwnedByYou",
		"Your previous request to create the named bucket succeeded and you already own it."}
	ErrMalformedXML = APIError{http.StatusBadRequest, "MalformedXML",
		"The XML you provided was not well-formed or did not validate against our published schema."}
	ErrInvalidLocationConstraint = APIError{http.StatusBadRequest, "InvalidLocationConstraint",
		"The specified location constraint is not valid."}
)

// errorBody is the S3 XML error document. Field order is element order.
type errorBody struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource"`
	RequestID string   `xml:"RequestId"`
}

// WriteError writes e as an S3 XML error response. It uses the request ID
// NewHandler assigned (a new one when r did not come through NewHandler) for
// both the x-amz-request-id header and the RequestId element. Resource is the
// decoded request path. HEAD responses get the status and headers only.
func WriteError(w http.ResponseWriter, r *http.Request, e APIError) {
	id := RequestID(r.Context())
	if id == "" {
		id = newRequestID()
	}
	h := w.Header()
	h.Set("Content-Type", "application/xml")
	h.Set("x-amz-request-id", id)
	w.WriteHeader(e.Status)
	if r.Method == http.MethodHead {
		return
	}

	// Headers are already sent, so a write or encode error here means the
	// client went away; there is nothing useful left to report to it.
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(errorBody{
		Code:      e.Code,
		Message:   e.Message,
		Resource:  r.URL.Path,
		RequestID: id,
	})
}
