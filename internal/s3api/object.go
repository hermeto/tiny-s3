package s3api

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/hermeto/tiny-s3/internal/awschunked"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// ObjectStore is the part of the storage engine the object operations use.
type ObjectStore interface {
	Put(ctx context.Context, bucket, key string, body io.Reader, size int64, opts storage.PutOptions) (storage.Info, error)
	Get(bucket, key string) (storage.Info, storage.Reader, error)
	Head(bucket, key string) (storage.Info, error)
}

// PutObjectOp returns the PutObject operation served by store. Every header
// check runs before the first body read, so a rejected request never makes
// an Expect: 100-continue client send its body.
func PutObjectOp(store ObjectStore, log *slog.Logger) Op {
	return func(w http.ResponseWriter, r *http.Request, t Target) {
		req, e, ok := parsePut(r)
		if !ok {
			WriteError(w, r, e)
			return
		}
		info, err := store.Put(r.Context(), t.Bucket, t.Key, req.body, req.size, req.opts)
		if err != nil {
			WriteError(w, r, storageError(log, r, "PutObject", t, err))
			return
		}
		w.Header().Set("ETag", `"`+info.MD5+`"`)
		w.WriteHeader(http.StatusOK)
	}
}

// copyBufferSize is the only per-download body buffer.
const copyBufferSize = 32 << 10

// GetObjectOp returns the GetObject operation served by store. The body is
// streamed from the version Get opened, so concurrent overwrites and deletes
// do not affect it. Any failure after the status is sent aborts the
// connection, so the client never sees a short success.
func GetObjectOp(store ObjectStore, log *slog.Logger) Op {
	return func(w http.ResponseWriter, r *http.Request, t Target) {
		info, rd, err := store.Get(t.Bucket, t.Key)
		if err != nil {
			WriteError(w, r, storageError(log, r, "GetObject", t, err))
			return
		}
		defer rd.Close()
		first, length, ok := startObjectResponse(w, r, info)
		if !ok {
			return
		}
		src := &readErrRecorder{r: io.NewSectionReader(rd, first, length)}
		if _, err := io.CopyBuffer(w, src, make([]byte, copyBufferSize)); err != nil {
			if src.err != nil {
				storageError(log, r, "GetObject", t, src.err)
			}
			// The status and length are sent: aborting is the only way
			// to tell the client the body is incomplete.
			panic(http.ErrAbortHandler)
		}
	}
}

// HeadObjectOp returns the HeadObject operation served by store: the same
// status and headers as GetObject, without a body.
func HeadObjectOp(store ObjectStore, log *slog.Logger) Op {
	return func(w http.ResponseWriter, r *http.Request, t Target) {
		info, err := store.Head(t.Bucket, t.Key)
		if err != nil {
			WriteError(w, r, storageError(log, r, "HeadObject", t, err))
			return
		}
		startObjectResponse(w, r, info)
	}
}

// startObjectResponse writes the status and headers of a GET or HEAD answer
// for info: 412/304 from the conditional headers, then 206 for a served
// Range (unless If-Range does not match), else 200. It returns the part of
// the body to send, or false when the response needs no body.
func startObjectResponse(w http.ResponseWriter, r *http.Request, info storage.Info) (first, length int64, ok bool) {
	if status := evaluateConditions(r.Header, info); status != http.StatusOK {
		writeConditional(w, r, status, info)
		return 0, 0, false
	}
	writeObjectHeaders(w.Header(), info)
	rng := parseRange(r.Header.Get("Range"), info.Size)
	if rng.kind == rangeServe {
		length = rng.last - rng.first + 1
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.first, rng.last, info.Size))
		w.WriteHeader(http.StatusPartialContent)
		return rng.first, length, r.Method == http.MethodGet
	}
	w.WriteHeader(http.StatusOK)
	return 0, info.Size, r.Method == http.MethodGet
}

// readErrRecorder remembers a read error, to tell a storage failure from a
// client that went away. It also hides the file's WriterTo, so the copy uses
// the fixed buffer.
type readErrRecorder struct {
	r   io.Reader
	err error
}

func (e *readErrRecorder) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF {
		e.err = err
	}
	return n, err
}

// writeObjectHeaders sets the metadata headers of a GET or HEAD response.
func writeObjectHeaders(h http.Header, info storage.Info) {
	h.Set("ETag", `"`+info.MD5+`"`)
	h.Set("Content-Length", strconv.FormatInt(info.Size, 10))
	h.Set("Content-Type", info.ContentType)
	h.Set("Last-Modified", lastModified(info))
	h.Set("Accept-Ranges", "bytes")
	for name, v := range info.Metadata {
		// Not h.Set: S3 sends these names in lowercase, and clients keep
		// the case they receive as the metadata key.
		h["x-amz-meta-"+name] = []string{v}
	}
}

// lastModified formats the object's time as Last-Modified does: RFC 1123 in
// GMT, whole seconds.
func lastModified(info storage.Info) string {
	return info.LastModified.UTC().Format(http.TimeFormat)
}

// putRequest is a checked PutObject request: the payload reader, its size
// and the storage options.
type putRequest struct {
	body io.Reader
	size int64
	opts storage.PutOptions
}

// noTrailers is the trailer lookup of a plain (unframed) body.
func noTrailers(string) (string, bool) { return "", false }

// parsePut checks the PutObject headers, in the order of research R5 (008),
// and returns the request to store or the error to answer with. It never
// reads the body: an aws-chunked body is decoded as Store.Put reads it, and
// a client checksum is verified by the Verify callback before the commit.
func parsePut(r *http.Request) (putRequest, APIError, bool) {
	req := putRequest{body: r.Body, size: r.ContentLength}
	trailer := noTrailers
	if isAWSChunked(r.Header) {
		v := r.Header.Get("X-Amz-Decoded-Content-Length")
		if v == "" {
			return putRequest{}, ErrMissingContentLength, false
		}
		size, err := strconv.ParseInt(v, 10, 64)
		if err != nil || size < 0 {
			return putRequest{}, ErrInvalidArgument, false
		}
		dec := awschunked.NewReader(r.Body)
		req.body, req.size, trailer = dec, size, dec.Trailer
	} else if r.ContentLength < 0 {
		return putRequest{}, ErrMissingContentLength, false
	}
	if v := r.Header.Get("Content-MD5"); v != "" {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) != md5.Size {
			return putRequest{}, ErrInvalidDigest, false
		}
		req.opts.ContentMD5 = b
	}
	c, e, ok := requestChecksum(r.Header)
	if !ok {
		return putRequest{}, e, false
	}
	if c != nil {
		req.body, req.opts.Verify = c.wrap(req.body, trailer)
	}
	meta, ok := userMetadata(r.Header)
	if !ok {
		return putRequest{}, ErrMetadataTooLarge, false
	}
	req.opts.ContentType, req.opts.Metadata = r.Header.Get("Content-Type"), meta
	return req, APIError{}, true
}

// isAWSChunked reports whether the body uses the aws-chunked framing: the
// content encoding names it, or the content hash is a STREAMING- value.
func isAWSChunked(h http.Header) bool {
	for _, enc := range strings.Split(h.Get("Content-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(enc), "aws-chunked") {
			return true
		}
	}
	return strings.HasPrefix(h.Get("X-Amz-Content-Sha256"), "STREAMING-")
}

// metaPrefix is the user metadata header prefix.
const metaPrefix = "X-Amz-Meta-"

// maxMetadataSize is the S3 limit on user metadata: names (without the
// prefix) plus values, in bytes.
const maxMetadataSize = 2048

// userMetadata collects the x-amz-meta-* headers with lowercase names,
// joining repeated headers with ",". It returns nil when there are none, and
// false when they exceed maxMetadataSize.
func userMetadata(h http.Header) (map[string]string, bool) {
	var meta map[string]string
	size := 0
	for k, vs := range h {
		// Header names are case-insensitive; net/http canonicalizes the
		// ones it parses, but not every caller does.
		if len(k) <= len(metaPrefix) || !strings.EqualFold(k[:len(metaPrefix)], metaPrefix) {
			continue
		}
		name := k[len(metaPrefix):]
		if meta == nil {
			meta = make(map[string]string)
		}
		name = strings.ToLower(name)
		v := strings.Join(vs, ",")
		meta[name] = v
		size += len(name) + len(v)
	}
	return meta, size <= maxMetadataSize
}
