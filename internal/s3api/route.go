package s3api

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// subresources are the query keys that select an S3 operation other than the
// plain bucket or object one. None is implemented, so any of them answers
// NotImplemented. Matched case-sensitively, on presence, as S3 does. Every
// other query key (list parameters, response-* overrides, x-id, presign
// parameters) is ignored by routing.
var subresources = map[string]bool{
	"accelerate": true, "acl": true, "analytics": true, "attributes": true,
	"cors": true, "delete": true, "encryption": true, "intelligent-tiering": true,
	"inventory": true, "legal-hold": true, "lifecycle": true, "location": true,
	"logging": true, "metadataTable": true, "metrics": true, "notification": true,
	"object-lock": true, "ownershipControls": true, "partNumber": true, "policy": true,
	"policyStatus": true, "publicAccessBlock": true, "replication": true,
	"requestPayment": true, "restore": true, "retention": true, "select": true,
	"session": true, "tagging": true, "torrent": true, "uploadId": true,
	"uploads": true, "versionId": true, "versioning": true, "versions": true,
	"website": true,
}

func hasSubresource(q url.Values) bool {
	for k := range q {
		if subresources[k] {
			return true
		}
	}
	return false
}

// parseTarget splits a decoded path-style path. "/" is the service; the first
// segment is the bucket and the rest, after one "/", is the key, kept as is
// (no cleaning). It fails for an empty bucket on any other path and for names
// that are not valid UTF-8.
func parseTarget(path string) (Target, bool) {
	if path == "/" {
		return Target{}, true
	}
	rest, ok := strings.CutPrefix(path, "/")
	if !ok {
		return Target{}, false
	}
	bucket, key, _ := strings.Cut(rest, "/")
	if bucket == "" || !utf8.ValidString(bucket) || !utf8.ValidString(key) {
		return Target{}, false
	}
	return Target{Bucket: bucket, Key: key}, true
}

// route picks the operation for r. It returns the selected Ops field (which
// may be nil) and ok, or the error to answer with. r.URL.Path is used as
// net/http decoded it; it is never decoded again.
func route(ops *Ops, r *http.Request) (op Op, t Target, e APIError, ok bool) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPost, http.MethodDelete:
	default:
		return nil, t, ErrMethodNotAllowed, false
	}
	t, valid := parseTarget(r.URL.Path)
	if !valid {
		return nil, t, ErrInvalidURI, false
	}

	if t.Bucket == "" {
		if r.Method == http.MethodGet {
			return ops.ListBuckets, t, e, true
		}
		return nil, t, ErrMethodNotAllowed, false
	}

	// Malformed pairs are dropped; the well-formed ones still count.
	q, _ := url.ParseQuery(r.URL.RawQuery)
	if hasSubresource(q) {
		return nil, t, ErrNotImplemented, false
	}

	if t.Key == "" {
		switch r.Method {
		case http.MethodGet:
			if q.Get("list-type") == "2" {
				return ops.ListObjectsV2, t, e, true
			}
		case http.MethodPut:
			return ops.CreateBucket, t, e, true
		case http.MethodHead:
			return ops.HeadBucket, t, e, true
		}
		// ListObjects v1, DeleteBucket, PostObject/DeleteObjects.
		return nil, t, ErrNotImplemented, false
	}

	switch r.Method {
	case http.MethodPut:
		if _, copying := r.Header["X-Amz-Copy-Source"]; copying {
			return nil, t, ErrNotImplemented, false // CopyObject
		}
		return ops.PutObject, t, e, true
	case http.MethodGet:
		return ops.GetObject, t, e, true
	case http.MethodHead:
		return ops.HeadObject, t, e, true
	case http.MethodDelete:
		return ops.DeleteObject, t, e, true
	}
	// POST on an object: multipart, restore, select.
	return nil, t, ErrNotImplemented, false
}
