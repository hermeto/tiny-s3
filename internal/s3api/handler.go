package s3api

import (
	"log/slog"
	"net/http"
)

// Target is the path-style address of a request. Bucket is empty for the
// service (/); Key is empty for bucket-level requests. Both are decoded once.
type Target struct {
	Bucket string
	Key    string
}

// Op serves one S3 operation for an already routed request.
type Op func(w http.ResponseWriter, r *http.Request, t Target)

// Ops holds one handler per in-scope S3 operation. A nil field answers
// ErrNotImplemented; each operation feature sets its own field.
type Ops struct {
	ListBuckets   Op
	CreateBucket  Op
	HeadBucket    Op
	ListObjectsV2 Op
	PutObject     Op
	GetObject     Op
	HeadObject    Op
	DeleteObject  Op
}

// NewHandler returns the S3 request handler. It assigns every request an ID
// (x-amz-request-id header and RequestID), routes it to an Ops field or an S3
// error, and turns a panic in an Op into a logged 500 InternalError, or into
// an aborted connection when the response had already started.
func NewHandler(ops Ops, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("x-amz-request-id", id)
		r = r.WithContext(withRequestID(r.Context(), id))
		rw := &responseWriter{ResponseWriter: w}
		defer recoverPanic(rw, r, log)

		op, t, e, ok := route(&ops, r)
		switch {
		case !ok:
			WriteError(rw, r, e)
		case op == nil:
			WriteError(rw, r, ErrNotImplemented)
		default:
			op(rw, r, t)
		}
	})
}

func recoverPanic(rw *responseWriter, r *http.Request, log *slog.Logger) {
	v := recover()
	if v == nil {
		return
	}
	if v == http.ErrAbortHandler {
		panic(v)
	}
	log.Error("handler_panic",
		"request_id", RequestID(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
		"panic", v)
	if rw.wrote {
		// The status is on its way; aborting the connection is the only
		// way to tell the client the response is not complete.
		panic(http.ErrAbortHandler)
	}
	WriteError(rw, r, ErrInternalError)
}

// responseWriter records whether the response has started. Unwrap lets
// http.ResponseController reach the underlying writer (flush, deadlines).
type responseWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *responseWriter) WriteHeader(status int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(p)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
