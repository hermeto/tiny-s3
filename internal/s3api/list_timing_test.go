//go:build !race

// The race detector slows code 5–10×, which makes a wall-clock bound
// meaningless; make test and make cover check it, make race does not.

package s3api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// pageLister returns the same page for every call: the handler's share of a
// listing is measured without building a 10,000-object store.
type pageLister struct{ page storage.ListResult }

func (p pageLister) List(string, storage.ListOptions) storage.ListResult { return p.page }

// TestListResponseTiming checks the HTTP half of SC-004 (012): encoding a
// full 1,000-entry page within 50 ms. The engine half (finding the page among
// 10,000 keys, also 50 ms) is internal/storage TestListPageTiming.
func TestListResponseTiming(t *testing.T) {
	var page storage.ListResult
	for i := range 1000 {
		page.Objects = append(page.Objects, storage.ObjectSummary{
			Key: fmt.Sprintf("some/longer/prefix/key-%05d.txt", i), Size: int64(i),
			MD5: "5eb63bbbe01eeed093cb22bb8f5acdc3", LastModified: time.Now(),
		})
	}
	page.Truncated, page.Next = true, page.Objects[999].Key
	h := NewHandler(Ops{ListObjectsV2: ListObjectsV2Op(pageLister{page}, discardLogger())}, discardLogger())

	start := time.Now()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demo?list-type=2&encoding-type=url", nil))
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond || w.Code != http.StatusOK {
		t.Fatalf("status %d in %v, want 200 within 50ms", w.Code, elapsed)
	}
}
