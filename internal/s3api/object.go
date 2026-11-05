package s3api

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// ObjectStore is the part of the storage engine the object operations use.
type ObjectStore interface {
	Put(ctx context.Context, bucket, key string, body io.Reader, size int64, opts storage.PutOptions) (storage.Info, error)
}

// PutObjectOp returns the PutObject operation served by store. Every header
// check runs before the first body read, so a rejected request never makes
// an Expect: 100-continue client send its body.
func PutObjectOp(store ObjectStore, log *slog.Logger) Op {
	return func(w http.ResponseWriter, r *http.Request, t Target) {
		opts, e, ok := putOptions(r)
		if !ok {
			WriteError(w, r, e)
			return
		}
		info, err := store.Put(r.Context(), t.Bucket, t.Key, r.Body, r.ContentLength, opts)
		if err != nil {
			WriteError(w, r, storageError(log, r, "PutObject", t, err))
			return
		}
		w.Header().Set("ETag", `"`+info.MD5+`"`)
		w.WriteHeader(http.StatusOK)
	}
}

// putOptions checks the PutObject headers, in the order of research R4, and
// returns the storage options or the error to answer with. It never reads
// the body.
func putOptions(r *http.Request) (storage.PutOptions, APIError, bool) {
	if isAWSChunked(r.Header) {
		// Decoding comes with feature 008; storing the framing would
		// corrupt the object.
		return storage.PutOptions{}, ErrNotImplemented, false
	}
	if r.ContentLength < 0 {
		return storage.PutOptions{}, ErrMissingContentLength, false
	}
	var digest []byte
	if v := r.Header.Get("Content-MD5"); v != "" {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(b) != md5.Size {
			return storage.PutOptions{}, ErrInvalidDigest, false
		}
		digest = b
	}
	meta, ok := userMetadata(r.Header)
	if !ok {
		return storage.PutOptions{}, ErrMetadataTooLarge, false
	}
	return storage.PutOptions{ContentMD5: digest, ContentType: r.Header.Get("Content-Type"), Metadata: meta}, APIError{}, true
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
