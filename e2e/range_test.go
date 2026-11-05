//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
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
