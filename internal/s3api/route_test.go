package s3api

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// awsEscape percent-encodes a key the way the AWS SDKs build the request path:
// every byte except unreserved characters and "/" is escaped.
func awsEscape(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~', c == '/':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// keyServer runs NewHandler on a real server, so net/http parses the request
// target, and records every GetObject target.
func keyServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	ops, rec := recordingOps()
	srv := httptest.NewServer(NewHandler(ops, discardLogger()))
	t.Cleanup(srv.Close)
	return srv, rec
}

// rawGet sends a request line verbatim, bypassing net/url normalization in
// the client.
func rawGet(t *testing.T, srv *httptest.Server, target string) *http.Response {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n", target); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestKeysDecodedOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, sent, want string // sent "" means awsEscape(want)
	}{
		{"slashes", "", "a/b/c"},
		{"space", "", "with space"},
		{"unicode", "", "relatório/ação.txt"},
		{"cjk", "", "日本語/キー"},
		{"emoji", "", "emoji-😀"},
		{"percent", "", "100%"},
		{"encoded percent", "%2541", "%41"},
		{"plus literal", "a+b", "a+b"},
		{"plus encoded", "", "a+b"},
		{"encoded slash", "a%2Fb", "a/b"},
		{"hash", "", "#hash"},
		{"question mark", "", "q?mark"},
		{"trailing slash", "", "dir/"},
		{"double slash", "", "a//b"},
		{"dot segment", "", "./a"},
		{"dot dot inside", "", "a/../b"},
		{"dot dot leading", "", "../x"},
		{"trailing dot", "", "trailing."},
		{"semicolon", "", "semi;colon"},
		{"ampersand equals", "", "amp&er=sand"},
		{"tilde", "", "tilde~"},
		{"quotes", "", `quote"'`},
		{"angle brackets", "", "<angle>"},
		{"literal unencoded unicode", "ação", "ação"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, rec := keyServer(t)
			sent := tt.sent
			if sent == "" {
				sent = awsEscape(tt.want)
			}
			resp := rawGet(t, srv, "/my.bucket-1/"+sent)
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", resp.StatusCode)
			}
			calls := rec.taken()
			want := Target{Bucket: "my.bucket-1", Key: tt.want}
			if len(calls) != 1 || calls[0].op != "GetObject" || calls[0].target != want {
				t.Fatalf("calls = %+v, want GetObject %+v", calls, want)
			}
		})
	}
}

func TestEncodedBucket(t *testing.T) {
	t.Parallel()

	srv, rec := keyServer(t)
	rawGet(t, srv, "/my%2Ebucket/k")
	if calls := rec.taken(); len(calls) != 1 || calls[0].target != (Target{"my.bucket", "k"}) {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestAbsoluteFormTarget(t *testing.T) {
	t.Parallel()

	srv, rec := keyServer(t)
	resp := rawGet(t, srv, "http://"+srv.Listener.Addr().String()+"/b/a%20b")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if calls := rec.taken(); len(calls) != 1 || calls[0].target != (Target{"b", "a b"}) {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestErrorResourceIsDecodedPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(NewHandler(Ops{}, discardLogger()))
	t.Cleanup(srv.Close)
	resp := rawGet(t, srv, "/b/"+awsEscape("dir/relatório 100%&.txt"))
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if e := parseError(t, raw); e.Resource != "/b/dir/relatório 100%&.txt" {
		t.Fatalf("Resource = %q", e.Resource)
	}
}

func TestParseTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want Target
		ok   bool
	}{
		{"/", Target{}, true},
		{"/b", Target{Bucket: "b"}, true},
		{"/b/", Target{Bucket: "b"}, true},
		{"/b/k", Target{"b", "k"}, true},
		{"/b//", Target{"b", "/"}, true},
		{"/b/k/", Target{"b", "k/"}, true},
		{"", Target{}, false},
		{"*", Target{}, false},
		{"//", Target{}, false},
		{"//k", Target{}, false},
		{"/b/\xff", Target{}, false},
		{"/\xff", Target{}, false},
	}
	for _, tt := range tests {
		got, ok := parseTarget(tt.path)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseTarget(%q) = %+v, %v; want %+v, %v", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}

func TestHasSubresource(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"":                                  false,
		"list-type=2&prefix=a":              false,
		"x-id=GetObject":                    false,
		"ACL":                               false,
		"acl":                               true,
		"acl=":                              true,
		"prefix=a&tagging":                  true,
		"a=%zz&uploads":                     true,
		"versionId=null":                    true,
		"response-content-type=x":           false,
		"X-Amz-Signature=deadbeef":          false,
		url.Values{"delete": {""}}.Encode(): true,
	}
	for raw, want := range tests {
		q, _ := url.ParseQuery(raw)
		if got := hasSubresource(q); got != want {
			t.Errorf("hasSubresource(%q) = %v, want %v", raw, got, want)
		}
	}
}
