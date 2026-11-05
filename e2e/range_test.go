//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// getRange runs get-object with --range and returns the bytes written and the
// ContentRange the CLI reported ("" when absent).
func getRange(t *testing.T, addr, rng string) ([]byte, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	res := mustSucceed(t, addr, "get-object", "--bucket", "demo", "--key", "r.bin", "--range", rng, out)
	var f struct{ ContentRange string }
	if err := json.Unmarshal([]byte(res.stdout), &f); err != nil {
		t.Fatalf("output %q: %v", res.stdout, err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return got, f.ContentRange
}

// rangeFixture starts s3d with demo/r.bin holding 1000 random bytes.
func rangeFixture(t *testing.T) (*proc, []byte) {
	t.Helper()
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	body := randomBytes(t, 1000)
	putETag(t, p.addr, "--bucket", "demo", "--key", "r.bin", "--body", writeFile(t, body))
	return p, body
}

func TestRangeCLI(t *testing.T) {
	p, body := rangeFixture(t)
	for _, tt := range []struct {
		rng         string
		first, last int
	}{
		{"bytes=0-99", 0, 99},
		{"bytes=100-", 100, 999},
		{"bytes=-50", 950, 999},
		{"bytes=990-5000", 990, 999},
	} {
		got, cr := getRange(t, p.addr, tt.rng)
		if !bytes.Equal(got, body[tt.first:tt.last+1]) || cr != fmt.Sprintf("bytes %d-%d/1000", tt.first, tt.last) {
			t.Fatalf("%s: %d bytes, ContentRange %q", tt.rng, len(got), cr)
		}
	}
	p.stop(t)
}

func TestRangeUnsatisfiableCLI(t *testing.T) {
	p, _ := rangeFixture(t)
	mustFail(t, p.addr, "(InvalidRange)", "get-object", "--bucket", "demo", "--key", "r.bin",
		"--range", "bytes=1000-", filepath.Join(t.TempDir(), "out"))
	p.stop(t)
}

func TestRangeIgnoredCLI(t *testing.T) {
	p, body := rangeFixture(t)
	for _, rng := range []string{"bytes=0-1,5-6", "bytes=abc"} {
		got, cr := getRange(t, p.addr, rng)
		if !bytes.Equal(got, body) || cr != "" {
			t.Fatalf("%s: %d bytes, ContentRange %q; want the whole object", rng, len(got), cr)
		}
	}

	// The CLI has no --if-range; use a plain client.
	etag := putETag(t, p.addr, "--bucket", "demo", "--key", "r.bin", "--body", writeFile(t, body))
	for _, tt := range []struct {
		ifRange string
		status  int
	}{
		{`"stale"`, http.StatusOK},
		{etag, http.StatusPartialContent},
	} {
		req, _ := http.NewRequest(http.MethodGet, "http://"+p.addr+"/demo/r.bin", nil)
		req.Header.Set("Range", "bytes=0-9")
		req.Header.Set("If-Range", tt.ifRange)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Fatalf("If-Range %s: %d, want %d", tt.ifRange, resp.StatusCode, tt.status)
		}
	}
	p.stop(t)
}
