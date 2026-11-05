package s3api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/hermeto/tiny-s3/internal/awschunked"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// storageErrors maps storage engine errors to S3 errors, checked in order.
var storageErrors = []struct {
	err error
	api APIError
}{
	{storage.ErrInvalidBucketName, ErrInvalidBucketName},
	{storage.ErrNoSuchBucket, ErrNoSuchBucket},
	{storage.ErrBucketExists, ErrBucketAlreadyOwnedByYou},
	{storage.ErrKeyTooLong, ErrKeyTooLong},
	{storage.ErrNoSuchKey, ErrNoSuchKey},
	{storage.ErrEntityTooLarge, ErrEntityTooLarge},
	{storage.ErrBadDigest, ErrBadDigest},
	{storage.ErrIncompleteBody, ErrIncompleteBody},
	// The request context ends when the client goes away mid-upload.
	{context.Canceled, ErrIncompleteBody},
	{storage.ErrInvalidArgument, ErrInvalidArgument},
	// Client checksums, verified by PutOptions.Verify (feature 008).
	{errChecksumMismatch, ErrBadChecksum},
	{errInvalidChecksum, ErrInvalidRequest},
	// aws-chunked framing (feature 008).
	{awschunked.ErrMalformed, ErrInvalidRequest},
	{awschunked.ErrTruncated, ErrIncompleteBody},
}

// storageError returns the S3 error for err, a storage engine error from op
// on t. An error with no S3 meaning is logged as storage_error with the
// request ID and answers InternalError.
func storageError(log *slog.Logger, r *http.Request, op string, t Target, err error) APIError {
	for _, m := range storageErrors {
		if errors.Is(err, m.err) {
			return m.api
		}
	}
	log.Error("storage_error",
		"request_id", RequestID(r.Context()),
		"op", op,
		"bucket", t.Bucket,
		"key", t.Key,
		"error", err.Error())
	return ErrInternalError
}
