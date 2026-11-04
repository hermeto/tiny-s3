package s3api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

type requestIDKey struct{}

// newRequestID returns 16 uppercase hex characters. crypto/rand.Read never
// returns an error (Go 1.24+).
func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID that NewHandler assigned to the request,
// or "" when ctx did not come from it.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
