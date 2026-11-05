package s3api

import (
	"bytes"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// maxCreateBucketBody bounds the CreateBucket configuration a request may
// send; a real one is well under 1 KiB.
const maxCreateBucketBody = 64 << 10

// region is the only region tiny-s3 serves.
const region = "us-east-1"

// BucketStore is the part of the storage engine the bucket operations use.
type BucketStore interface {
	CreateBucket(name string) (storage.Bucket, error)
	HeadBucket(name string) (storage.Bucket, error)
	ListBuckets() []storage.Bucket
}

// BucketOps returns Ops with the bucket operations served by store. Storage
// errors without an S3 meaning answer InternalError and are logged as
// storage_error.
func BucketOps(store BucketStore, log *slog.Logger) Ops {
	b := bucketOps{store: store, log: log}
	return Ops{
		CreateBucket: b.create,
		HeadBucket:   b.head,
		ListBuckets:  b.list,
		CheckBucket:  b.check,
	}
}

type bucketOps struct {
	store BucketStore
	log   *slog.Logger
}

// check is the bucket gate (Ops.CheckBucket).
func (b bucketOps) check(r *http.Request, bucket string) (APIError, bool) {
	if _, err := b.store.HeadBucket(bucket); err != nil {
		return storageError(b.log, r, "CheckBucket", Target{Bucket: bucket}, err), false
	}
	return APIError{}, true
}

// head answers HeadBucket; the gate has already found the bucket.
func (b bucketOps) head(w http.ResponseWriter, _ *http.Request, _ Target) {
	w.Header().Set("x-amz-bucket-region", region)
	w.WriteHeader(http.StatusOK)
}

func (b bucketOps) create(w http.ResponseWriter, r *http.Request, t Target) {
	if !storage.ValidBucketName(t.Bucket) {
		WriteError(w, r, ErrInvalidBucketName)
		return
	}
	if e, ok := checkCreateBucketBody(r.Body); !ok {
		WriteError(w, r, e)
		return
	}
	if _, err := b.store.CreateBucket(t.Bucket); err != nil {
		WriteError(w, r, storageError(b.log, r, "CreateBucket", t, err))
		return
	}
	w.Header().Set("Location", "/"+t.Bucket)
	w.WriteHeader(http.StatusOK)
}

// listAllMyBucketsResult is the ListBuckets response. The S3 namespace is the
// default namespace of the root element.
type listAllMyBucketsResult struct {
	XMLName xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListAllMyBucketsResult"`
	Owner   struct {
		ID          string `xml:"ID"`
		DisplayName string `xml:"DisplayName"`
	} `xml:"Owner"`
	// A struct, not a "Buckets>Bucket" path, so <Buckets> is written even
	// when there are no buckets.
	Buckets struct {
		Bucket []bucketEntry `xml:"Bucket"`
	} `xml:"Buckets"`
}

type bucketEntry struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// owner is the fixed owner of every bucket: tiny-s3 has no accounts.
const owner = "tiny-s3"

// s3Time is the S3 timestamp format: UTC with milliseconds.
const s3Time = "2006-01-02T15:04:05.000Z"

// list answers ListBuckets. Query parameters (prefix, paging) are ignored:
// every bucket is returned in one response, sorted by name.
func (b bucketOps) list(w http.ResponseWriter, _ *http.Request, _ Target) {
	var res listAllMyBucketsResult
	res.Owner.ID, res.Owner.DisplayName = owner, owner
	res.Buckets.Bucket = []bucketEntry{}
	for _, bk := range b.store.ListBuckets() {
		res.Buckets.Bucket = append(res.Buckets.Bucket, bucketEntry{
			Name:         bk.Name,
			CreationDate: bk.CreationDate.UTC().Format(s3Time),
		})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	// The status is sent; a write error means the client went away.
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(res)
}

// createBucketConfiguration is the optional CreateBucket request body. The
// root element is matched by local name, whatever its namespace.
type createBucketConfiguration struct {
	XMLName            xml.Name `xml:"CreateBucketConfiguration"`
	LocationConstraint string   `xml:"LocationConstraint"`
}

// checkCreateBucketBody accepts no body, an empty one, or a configuration
// whose location is absent, empty or us-east-1.
func checkCreateBucketBody(body io.Reader) (APIError, bool) {
	raw, err := io.ReadAll(io.LimitReader(body, maxCreateBucketBody+1))
	if err != nil || len(raw) > maxCreateBucketBody {
		return ErrMalformedXML, false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return APIError{}, true
	}
	var cfg createBucketConfiguration
	if err := xml.Unmarshal(raw, &cfg); err != nil {
		return ErrMalformedXML, false
	}
	switch cfg.LocationConstraint {
	case "", region:
		return APIError{}, true
	}
	return ErrInvalidLocationConstraint, false
}
