package s3api_test

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/s3api"
)

const notImplementedMessage = "A header or method you provided implies functionality that is not implemented."

func TestHandlerNotImplemented(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(s3api.NewHandler())
	t.Cleanup(srv.Close)

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead}
	paths := []string{"/", "/bucket", "/bucket/key/with/slashes"}

	for _, method := range methods {
		for _, path := range paths {
			t.Run(method+" "+path, func(t *testing.T) {
				t.Parallel()

				var body io.Reader
				if method == http.MethodPut {
					body = strings.NewReader("payload")
				}
				req, err := http.NewRequest(method, srv.URL+path, body)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusNotImplemented {
					t.Fatalf("status = %d, want 501", resp.StatusCode)
				}
				raw, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if method == http.MethodHead {
					if len(raw) != 0 {
						t.Fatalf("HEAD body = %q, want empty", raw)
					}
					return
				}
				var e errorBody
				if err := xml.Unmarshal(raw, &e); err != nil {
					t.Fatalf("unmarshal %q: %v", raw, err)
				}
				if e.Code != "NotImplemented" || e.Message != notImplementedMessage || e.Resource != path {
					t.Fatalf("body = %+v", e)
				}
			})
		}
	}
}
