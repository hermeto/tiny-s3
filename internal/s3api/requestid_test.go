package s3api

import (
	"context"
	"regexp"
	"testing"
)

var requestIDPattern = regexp.MustCompile(`^[0-9A-F]{16}$`)

func TestNewRequestID(t *testing.T) {
	t.Parallel()

	first, second := newRequestID(), newRequestID()
	for _, id := range []string{first, second} {
		if !requestIDPattern.MatchString(id) {
			t.Errorf("request ID %q does not match %s", id, requestIDPattern)
		}
	}
	if first == second {
		t.Fatalf("request IDs repeat: %q", first)
	}
}

func TestRequestIDContext(t *testing.T) {
	t.Parallel()

	if got := RequestID(context.Background()); got != "" {
		t.Fatalf("RequestID(bare context) = %q, want empty", got)
	}
	ctx := withRequestID(context.Background(), "0123456789ABCDEF")
	if got := RequestID(ctx); got != "0123456789ABCDEF" {
		t.Fatalf("RequestID = %q, want 0123456789ABCDEF", got)
	}
}
