//go:build !race

// The race detector slows code 5–10×, which makes a wall-clock bound
// meaningless; make test and make cover check it, make race does not.

package storage

import (
	"fmt"
	"testing"
	"time"
)

// TestListPageTiming checks the engine half of SC-004 (012): a 1,000-key page
// from a bucket of 10,000 keys within 50 ms. The HTTP half (encoding the page,
// also 50 ms) is internal/s3api TestListResponseTiming.
func TestListPageTiming(t *testing.T) {
	s, _ := newStore(t, nil)
	for i := range 10000 {
		s.index.put("b", fmt.Sprintf("key-%05d", i), entry{size: 1})
	}
	start := time.Now()
	r := s.List("b", ListOptions{After: "key-04999", MaxKeys: 1000})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond || len(r.Objects) != 1000 {
		t.Fatalf("listed %d keys in %v, want 1000 within 50ms", len(r.Objects), elapsed)
	}
}
