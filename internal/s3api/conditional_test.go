package s3api

import (
	"net/http"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/storage"
)

func TestEvaluateConditions(t *testing.T) {
	t.Parallel()
	modified := time.Date(2026, 11, 4, 14, 30, 15, 900_000_000, time.UTC) // truncates to :15
	info := storage.Info{MD5: "5eb63bbbe01eeed093cb22bb8f5acdc3", LastModified: modified}
	etag := `"5eb63bbbe01eeed093cb22bb8f5acdc3"`
	other := `"00000000000000000000000000000000"`
	at := func(d time.Duration) string { return modified.Truncate(time.Second).Add(d).Format(http.TimeFormat) }

	tests := []struct {
		name   string
		header http.Header
		want   int
	}{
		{"none", http.Header{}, 200},
		{"if-match same", http.Header{"If-Match": {etag}}, 200},
		{"if-match other", http.Header{"If-Match": {other}}, 412},
		{"if-match star", http.Header{"If-Match": {"*"}}, 200},
		{"if-match list", http.Header{"If-Match": {other + ", " + etag}}, 200},
		{"if-match unquoted", http.Header{"If-Match": {"5eb63bbbe01eeed093cb22bb8f5acdc3"}}, 200},
		{"if-match weak", http.Header{"If-Match": {"W/" + etag}}, 200},
		{"if-unmodified-since before", http.Header{"If-Unmodified-Since": {at(-time.Second)}}, 412},
		{"if-unmodified-since equal", http.Header{"If-Unmodified-Since": {at(0)}}, 200},
		{"if-unmodified-since bad date", http.Header{"If-Unmodified-Since": {"yesterday"}}, 200},
		{"if-none-match same", http.Header{"If-None-Match": {etag}}, 304},
		{"if-none-match other", http.Header{"If-None-Match": {other}}, 200},
		{"if-none-match star", http.Header{"If-None-Match": {"*"}}, 304},
		{"if-modified-since future", http.Header{"If-Modified-Since": {at(24 * time.Hour)}}, 304},
		{"if-modified-since equal", http.Header{"If-Modified-Since": {at(0)}}, 304},
		{"if-modified-since past", http.Header{"If-Modified-Since": {at(-time.Second)}}, 200},
		{"if-modified-since rfc850", http.Header{"If-Modified-Since": {modified.Add(time.Hour).Format(time.RFC850)}}, 304},
		{"if-modified-since bad date", http.Header{"If-Modified-Since": {"tomorrow"}}, 200},
		// S3's documented special cases.
		{"if-match true beats if-unmodified-since false", http.Header{"If-Match": {etag}, "If-Unmodified-Since": {at(-time.Hour)}}, 200},
		{"if-none-match false beats if-modified-since true", http.Header{"If-None-Match": {etag}, "If-Modified-Since": {at(-time.Hour)}}, 304},
		{"if-none-match true ignores if-modified-since", http.Header{"If-None-Match": {other}, "If-Modified-Since": {at(time.Hour)}}, 200},
		{"412 before 304", http.Header{"If-Match": {other}, "If-None-Match": {etag}}, 412},
	}
	for _, tt := range tests {
		if got := evaluateConditions(tt.header, info); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}
