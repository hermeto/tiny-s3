package s3api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermeto/tiny-s3/internal/storage"
)

func TestStorageError(t *testing.T) {
	t.Parallel()
	wrap := func(err error) error { return fmt.Errorf("storage: copy body: %w", err) }
	tests := []struct {
		err  error
		want APIError
	}{
		{storage.ErrInvalidBucketName, ErrInvalidBucketName},
		{storage.ErrNoSuchBucket, ErrNoSuchBucket},
		{storage.ErrBucketExists, ErrBucketAlreadyOwnedByYou},
		{storage.ErrKeyTooLong, ErrKeyTooLong},
		{storage.ErrEntityTooLarge, ErrEntityTooLarge},
		{storage.ErrBadDigest, ErrBadDigest},
		{storage.ErrIncompleteBody, ErrIncompleteBody},
		{wrap(context.Canceled), ErrIncompleteBody},
		{fmt.Errorf("%w: empty key", storage.ErrInvalidArgument), ErrInvalidArgument},
	}
	for _, tt := range tests {
		logs := &logBuffer{}
		log := slog.New(slog.NewJSONHandler(logs, nil))
		r := httptest.NewRequest(http.MethodPut, "/b/k", nil)
		if got := storageError(log, r, "PutObject", Target{"bucket", "key"}, tt.err); got != tt.want {
			t.Errorf("storageError(%v) = %v, want %v", tt.err, got.Code, tt.want.Code)
		}
		if recs := logs.records(t); len(recs) != 0 {
			t.Errorf("storageError(%v) logged %v", tt.err, recs)
		}
	}
}

func TestStorageErrorUnknownIsLogged(t *testing.T) {
	t.Parallel()
	logs := &logBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	r := httptest.NewRequest(http.MethodPut, "/b/k", nil)
	r = r.WithContext(withRequestID(r.Context(), "ABC"))
	got := storageError(log, r, "PutObject", Target{"bucket", "key"}, errors.New("disk on fire"))
	if got != ErrInternalError {
		t.Fatalf("got %v, want InternalError", got.Code)
	}
	recs := logs.records(t)
	if len(recs) != 1 {
		t.Fatalf("log = %v", recs)
	}
	rec := recs[0]
	if rec["msg"] != "storage_error" || rec["level"] != "ERROR" || rec["request_id"] != "ABC" || rec["op"] != "PutObject" ||
		rec["bucket"] != "bucket" || rec["key"] != "key" || rec["error"] != "disk on fire" {
		t.Fatalf("log record = %v", rec)
	}
}
