package s3api

import "net/http"

const notImplementedMessage = "A header or method you provided implies functionality that is not implemented."

// NewHandler returns the S3 request handler. No S3 operation exists yet, so
// every request gets a 501 NotImplemented error; each operation feature
// replaces this response for the routes it implements.
func NewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotImplemented, "NotImplemented", notImplementedMessage)
	})
}
