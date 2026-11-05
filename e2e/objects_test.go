//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeFile creates a file with content in a temp dir and returns its path.
func writeFile(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func quotedMD5(b []byte) string {
	sum := md5.Sum(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// putETag runs put-object and returns the ETag it printed.
func putETag(t *testing.T, addr string, args ...string) string {
	t.Helper()
	res := mustSucceed(t, addr, append([]string{"put-object"}, args...)...)
	var out struct{ ETag string }
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("put-object output %q: %v", res.stdout, err)
	}
	return out.ETag
}

func TestPutObjectCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")

	v1 := []byte("hello world")
	if got, want := putETag(t, p.addr, "--bucket", "demo", "--key", "a/b.txt", "--body", writeFile(t, v1)), quotedMD5(v1); got != want {
		t.Fatalf("ETag = %s, want %s", got, want)
	}
	v2 := []byte("a different, longer body")
	if got, want := putETag(t, p.addr, "--bucket", "demo", "--key", "a/b.txt", "--body", writeFile(t, v2)), quotedMD5(v2); got != want {
		t.Fatalf("overwrite ETag = %s, want %s", got, want)
	}
	if got, want := putETag(t, p.addr, "--bucket", "demo", "--key", "typed.txt", "--body", writeFile(t, v1),
		"--content-type", "text/plain", "--metadata", "owner=alice,team=core"), quotedMD5(v1); got != want {
		t.Fatalf("ETag with metadata = %s, want %s", got, want)
	}
	mustFail(t, p.addr, "(NoSuchBucket)", "put-object", "--bucket", "missing", "--key", "k", "--body", writeFile(t, v1))
	p.stop(t)
}

func TestPutObjectRejectsCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	body := writeFile(t, []byte("hello world"))
	mustFail(t, p.addr, "(BadDigest)", "put-object", "--bucket", "demo", "--key", "k", "--body", body,
		"--content-md5", "AAAAAAAAAAAAAAAAAAAAAA==")
	mustFail(t, p.addr, "(KeyTooLongError)", "put-object", "--bucket", "demo", "--key", strings.Repeat("k", 1025), "--body", body)

	// What the CLI cannot send: an early disconnect and a body without a length.
	for _, tt := range []struct {
		raw    string
		status int
		code   string
	}{
		{"PUT /demo/short HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\n0123456789", 400, "IncompleteBody"},
		{"PUT /demo/chunked HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n", 411, "MissingContentLength"},
	} {
		conn, err := net.Dial("tcp", p.addr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(conn, tt.raw); err != nil {
			t.Fatal(err)
		}
		_ = conn.(*net.TCPConn).CloseWrite()
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = conn.Close()
		if resp.StatusCode != tt.status || !strings.Contains(string(raw), "<Code>"+tt.code+"</Code>") {
			t.Fatalf("%q: got %d %q, want %d %s", tt.raw[:20], resp.StatusCode, raw, tt.status, tt.code)
		}
	}
	p.stop(t)
}

// patternReader yields n bytes of a fixed pattern without holding them. Each
// byte depends only on its position, so any part of the stream can be
// regenerated whatever the read sizes.
type patternReader struct{ n, off int64 }

func (r *patternReader) Read(b []byte) (int, error) {
	if r.n == 0 {
		return 0, io.EOF
	}
	if int64(len(b)) > r.n {
		b = b[:r.n]
	}
	for i := range b {
		b[i] = byte((r.off + int64(i)) % 251)
	}
	r.n -= int64(len(b))
	r.off += int64(len(b))
	return len(b), nil
}

// peakRSS returns VmHWM of pid in bytes (Linux only).
func peakRSS(t *testing.T, pid int) int64 {
	t.Helper()
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatalf("read status (the e2e tests run on Linux in make e2e): %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			kb, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return kb << 10
		}
	}
	t.Fatal("no VmHWM in status")
	return 0
}

// TestPutObjectLargeFlatMemory uploads 2 GiB and checks the server's peak RSS.
func TestPutObjectLargeFlatMemory(t *testing.T) {
	const size = 2 << 30
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")

	h := md5.New()
	body := io.TeeReader(&patternReader{n: size}, h)
	req, err := http.NewRequest(http.MethodPut, "http://"+p.addr+"/demo/big", body)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = size
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if want := `"` + hex.EncodeToString(h.Sum(nil)) + `"`; resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") != want {
		t.Fatalf("got %d, ETag %q, want 200 %s", resp.StatusCode, resp.Header.Get("ETag"), want)
	}
	if rss := peakRSS(t, p.cmd.Process.Pid); rss >= 64<<20 {
		t.Fatalf("peak RSS after upload %d MiB, want below 64 MiB", rss>>20)
	}

	// Download it back (feature 009) and check the bytes and the memory.
	get, err := http.Get("http://" + p.addr + "/demo/big")
	if err != nil {
		t.Fatal(err)
	}
	h2 := md5.New()
	n, err := io.Copy(h2, get.Body)
	_ = get.Body.Close()
	if err != nil || n != size || !bytes.Equal(h2.Sum(nil), h.Sum(nil)) {
		t.Fatalf("download: %d bytes, %v; MD5 match %v", n, err, bytes.Equal(h2.Sum(nil), h.Sum(nil)))
	}
	if rss := peakRSS(t, p.cmd.Process.Pid); rss >= 64<<20 {
		t.Fatalf("peak RSS after download %d MiB, want below 64 MiB", rss>>20)
	}

	// Its second half as a range (feature 010).
	half := &patternReader{n: size}
	if _, err := io.CopyN(io.Discard, half, size/2); err != nil {
		t.Fatal(err)
	}
	wantHalf := md5.New()
	if _, err := io.Copy(wantHalf, half); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodGet, "http://"+p.addr+"/demo/big", nil)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", size/2))
	ranged, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	gotHalf := md5.New()
	n, err = io.Copy(gotHalf, ranged.Body)
	_ = ranged.Body.Close()
	if err != nil || ranged.StatusCode != http.StatusPartialContent || n != size/2 || !bytes.Equal(gotHalf.Sum(nil), wantHalf.Sum(nil)) {
		t.Fatalf("range: status %d, %d bytes, %v", ranged.StatusCode, n, err)
	}
	if rss := peakRSS(t, p.cmd.Process.Pid); rss >= 64<<20 {
		t.Fatalf("peak RSS after the range %d MiB, want below 64 MiB", rss>>20)
	}
	p.stop(t)
}

// TestGetSurvivesOverwrite overwrites a key over HTTP while a download of it
// is half read; the download still returns the original bytes.
func TestGetSurvivesOverwrite(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	original := randomBytes(t, 8<<20)
	putETag(t, p.addr, "--bucket", "demo", "--key", "k", "--body", writeFile(t, original))

	resp, err := http.Get("http://" + p.addr + "/demo/k")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	first := make([]byte, 1<<20)
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatal(err)
	}
	putETag(t, p.addr, "--bucket", "demo", "--key", "k", "--body", writeFile(t, []byte("new content")))
	rest, err := io.ReadAll(resp.Body)
	if err != nil || !bytes.Equal(append(first, rest...), original) {
		t.Fatalf("download during overwrite: %d bytes, %v; want the original", len(first)+len(rest), err)
	}
	p.stop(t)
}
