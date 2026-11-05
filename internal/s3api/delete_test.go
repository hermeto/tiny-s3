package s3api

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// deleteHandler serves the object operations including DeleteObject.
func deleteHandler(store ObjectStore, log *slog.Logger) http.Handler {
	ops := readOps(store, log)
	ops.DeleteObject = DeleteObjectOp(store, log)
	return NewHandler(ops, log)
}

func TestDeleteObject(t *testing.T) {
	t.Parallel()
	_, store, root, _ := readStore(t, nil)
	h := deleteHandler(store, discardLogger())
	mustPut(t, h, "k", "hello", nil)

	for i := range 2 { // existing, then already gone
		w := serveReq(h, httptest.NewRequest(http.MethodDelete, "/demo/k", nil))
		if w.Code != http.StatusNoContent || w.Body.Len() != 0 || len(w.Header()) != 1 || w.Header().Get("x-amz-request-id") == "" {
			t.Fatalf("delete %d: %d %q %v", i, w.Code, w.Body.String(), w.Header())
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		wantError(t, serveReq(h, httptest.NewRequest(method, "/demo/k", nil)), method, ErrNoSuchKey)
	}

	// Conditional headers are ignored.
	mustPut(t, h, "c", "x", nil)
	r := httptest.NewRequest(http.MethodDelete, "/demo/c", nil)
	r.Header.Set("If-Match", `"0000"`)
	if w := serveReq(h, r); w.Code != http.StatusNoContent {
		t.Fatalf("conditional delete = %d", w.Code)
	}

	// A corrupt record is removed.
	mustPut(t, h, "bad", "x", nil)
	des, _ := os.ReadDir(filepath.Join(root, "meta"))
	for _, de := range des {
		if err := os.WriteFile(filepath.Join(root, "meta", de.Name()), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if w := serveReq(h, httptest.NewRequest(http.MethodDelete, "/demo/bad", nil)); w.Code != http.StatusNoContent {
		t.Fatalf("delete of a corrupt record = %d", w.Code)
	}
	wantError(t, serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/bad", nil)), http.MethodGet, ErrNoSuchKey)

	wantError(t, serveReq(h, httptest.NewRequest(http.MethodDelete, "/demo/"+strings.Repeat("k", 1025), nil)), http.MethodDelete, ErrKeyTooLong)
}

// TestDeleteObjectStorageFailures covers the two failure points of a delete:
// removing the record (the object stays readable) and syncing meta/ after
// the removal (the body is kept, so a record that reappears is complete).
func TestDeleteObjectStorageFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		fault    func(root string) fsx.Fault
		readable bool
	}{
		{"remove record", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpRemove, PathPattern: filepath.Join(root, "meta", "*"), Err: errors.New("remove failed")}
		}, true},
		{"sync meta", func(root string) fsx.Fault {
			return fsx.Fault{Op: fsx.OpDirSync, PathPattern: filepath.Join(root, "meta"), Err: errors.New("sync failed")}
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h0, _, root, _ := readStore(t, nil)
			mustPut(t, h0, "k", "hello", nil)
			store, err := storage.Open(fsx.NewFaulty(fsx.OS{}, tt.fault(root)), root)
			if err != nil {
				t.Fatal(err)
			}
			logs := &logBuffer{}
			log := slog.New(slog.NewJSONHandler(logs, nil))
			h := deleteHandler(store, log)
			wantError(t, serveReq(h, httptest.NewRequest(http.MethodDelete, "/demo/k", nil)), http.MethodDelete, ErrInternalError)
			if recs := logs.records(t); len(recs) != 1 || recs[0]["msg"] != "storage_error" || recs[0]["op"] != "DeleteObject" {
				t.Fatalf("log = %v", recs)
			}
			if des, _ := os.ReadDir(filepath.Join(root, "data")); len(des) != 1 {
				t.Fatalf("data = %v, want the body kept", des)
			}
			if tt.readable {
				w := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo/k", nil))
				if w.Code != http.StatusOK || w.Body.String() != "hello" {
					t.Fatalf("GET after a failed delete = %d %q", w.Code, w.Body.String())
				}
			}
		})
	}
}
