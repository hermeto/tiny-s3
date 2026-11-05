//go:build e2e

package e2e

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// tlsProxy is an HTTPS endpoint forwarding to s3d. The aws CLI uses its
// default aws-chunked framing only over HTTPS; s3d serves plain HTTP.
type tlsProxy struct {
	endpoint string
	env      []string      // AWS_CA_BUNDLE pointing at the proxy certificate
	framed   *atomic.Int64 // requests forwarded with Content-Encoding aws-chunked
}

func startTLSProxy(t *testing.T, addr string) tlsProxy {
	t.Helper()
	target, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	framed := &atomic.Int64{}
	proxy := httputil.NewSingleHostReverseProxy(target)
	forward := proxy.Director
	proxy.Director = func(r *http.Request) {
		if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
			framed.Add(1)
		}
		forward(r)
	}
	srv := httptest.NewTLSServer(proxy)
	t.Cleanup(srv.Close)
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(bundle, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return tlsProxy{endpoint: srv.URL, env: []string{"AWS_CA_BUNDLE=" + bundle}, framed: framed}
}

// putVia uploads file through endpoint and returns the ETag.
func putVia(t *testing.T, endpoint string, env []string, args ...string) string {
	t.Helper()
	res := awsCLIAt(t, endpoint, env, append([]string{"put-object"}, args...)...)
	if res.code != 0 {
		t.Fatalf("put-object %v: exit %d, stderr %q", args, res.code, res.stderr)
	}
	var out struct{ ETag string }
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("put-object output %q: %v", res.stdout, err)
	}
	return out.ETag
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDefaultCLIUploads uploads with the CLI's default settings over HTTPS
// (aws-chunked with a CRC64NVME trailer) and over plain HTTP (checksum
// header), plus each --checksum-algorithm. The ETag is the MD5 the server
// computed over the bytes it stored, so equality proves the framing was
// removed exactly.
func TestDefaultCLIUploads(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	proxy := startTLSProxy(t, p.addr)

	files := map[string][]byte{
		"empty":  {},
		"small":  []byte("hello world"),
		"10 MiB": randomBytes(t, 10<<20),
	}
	for name, content := range files {
		path := writeFile(t, content)
		want := quotedMD5(content)
		if got := putVia(t, proxy.endpoint, proxy.env, "--bucket", "demo", "--key", "https/"+name, "--body", path); got != want {
			t.Fatalf("%s over HTTPS: ETag %s, want %s", name, got, want)
		}
		if got := putVia(t, "http://"+p.addr, nil, "--bucket", "demo", "--key", "http/"+name, "--body", path); got != want {
			t.Fatalf("%s over HTTP: ETag %s, want %s", name, got, want)
		}
	}

	// Every HTTPS upload above must have used the framing, or this test
	// proves nothing about the decoder.
	if got := proxy.framed.Load(); got != int64(len(files)) {
		t.Fatalf("%d of %d HTTPS uploads used aws-chunked", got, len(files))
	}

	small := writeFile(t, []byte("hello world"))
	for _, alg := range []string{"CRC32", "CRC32C", "SHA1", "SHA256"} {
		if got := putVia(t, proxy.endpoint, proxy.env, "--bucket", "demo", "--key", alg, "--body", small,
			"--checksum-algorithm", alg); got != quotedMD5([]byte("hello world")) {
			t.Fatalf("%s: ETag %s", alg, got)
		}
	}
	p.stop(t)
}

// putFramed sends an aws-chunked PUT of stream (decoded length 11) and
// returns the status and body. With cut, the declared Content-Length is 100
// bytes more than the stream and the connection is half-closed after it.
func putFramed(t *testing.T, addr, key, stream, contentSHA string, header map[string]string, cut bool) (int, string) {
	t.Helper()
	length := len(stream)
	if cut {
		length += 100
	}
	var b strings.Builder
	fmt.Fprintf(&b, "PUT /demo/%s HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n", key, length)
	fmt.Fprintf(&b, "Content-Encoding: aws-chunked\r\nX-Amz-Content-Sha256: %s\r\nX-Amz-Decoded-Content-Length: 11\r\n", contentSHA)
	for k, v := range header {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("Connection: close\r\n\r\n")
	b.WriteString(stream)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, b.String()); err != nil {
		t.Fatal(err)
	}
	if cut {
		// Only a cut request half-closes: net/http treats a half-close as
		// the client leaving and cancels the request context, which would
		// make a complete upload fail at random.
		_ = conn.(*net.TCPConn).CloseWrite()
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("ETag") + string(raw)
}

func TestFramedUploadsRaw(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	sig := ";chunk-signature=" + strings.Repeat("ab", 32)
	trailer := map[string]string{"X-Amz-Trailer": "x-amz-checksum-crc64nvme"}
	const hello = `"5eb63bbbe01eeed093cb22bb8f5acdc3"`

	tests := []struct {
		name, stream, sha string
		header            map[string]string
		cut               bool
		status            int
		want              string // ETag on success, else the error code
	}{
		{"signed, signatures not verified", "5" + sig + "\r\nhello\r\n6" + sig + "\r\n world\r\n0" + sig + "\r\n\r\n",
			"STREAMING-AWS4-HMAC-SHA256-PAYLOAD", nil, false, 200, hello},
		{"unsigned with trailer", "b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:jSnVw/bqjr4=\r\n\r\n",
			"STREAMING-UNSIGNED-PAYLOAD-TRAILER", trailer, false, 200, hello},
		{"wrong trailer", "b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:AAAAAAAAAAA=\r\n\r\n",
			"STREAMING-UNSIGNED-PAYLOAD-TRAILER", trailer, false, 400, "BadDigest"},
		{"truncated", "b\r\nhello", "STREAMING-UNSIGNED-PAYLOAD-TRAILER", nil, true, 400, "IncompleteBody"},
		{"malformed header", "zz\r\nhello world\r\n0\r\n\r\n", "STREAMING-UNSIGNED-PAYLOAD-TRAILER", nil, false, 400, "InvalidRequest"},
	}
	for _, tt := range tests {
		status, got := putFramed(t, p.addr, "raw", tt.stream, tt.sha, tt.header, tt.cut)
		if status != tt.status || !strings.Contains(got, tt.want) {
			t.Fatalf("%s: got %d %q, want %d %s", tt.name, status, got, tt.status, tt.want)
		}
	}

	// A wrong checksum header through the CLI over plain HTTP.
	mustFail(t, p.addr, "(BadDigest)", "put-object", "--bucket", "demo", "--key", "bad",
		"--body", writeFile(t, []byte("hello world")), "--checksum-crc32", "AAAAAA==")
	p.stop(t)
}
