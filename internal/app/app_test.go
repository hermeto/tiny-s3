package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/config"
	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/storage"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// running is a Main or run call in progress, with its stdout decoded line by line.
type running struct {
	cancel context.CancelFunc
	stderr *syncBuffer
	exit   chan int
	lines  chan map[string]any
}

func launch(t *testing.T, call func(ctx context.Context, stdout, stderr io.Writer) int) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pr, pw := io.Pipe()
	r := &running{cancel: cancel, stderr: &syncBuffer{}, exit: make(chan int, 1), lines: make(chan map[string]any, 64)}
	go func() {
		defer close(r.lines)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			var rec map[string]any
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				rec = map[string]any{"raw": sc.Text()}
			}
			r.lines <- rec
		}
	}()
	go func() {
		code := call(ctx, pw, r.stderr)
		_ = pw.Close()
		r.exit <- code
	}()
	return r
}

// waitFor returns the first stdout record with the given msg.
func (r *running) waitFor(t *testing.T, msg string) map[string]any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case rec, ok := <-r.lines:
			if !ok {
				t.Fatalf("stdout closed before %q; stderr: %s", msg, r.stderr.String())
			}
			if rec["msg"] == msg {
				return rec
			}
		case <-timeout:
			t.Fatalf("no %q record within 5s; stderr: %s", msg, r.stderr.String())
		}
	}
}

func (r *running) code(t *testing.T) int {
	t.Helper()
	select {
	case c := <-r.exit:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("did not exit within 5s")
		return -1
	}
}

func TestMainServesAndStops(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := launch(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return Main(ctx, []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, stdout, stderr, fsx.OS{})
	})

	rec := r.waitFor(t, "listening")
	addr, _ := rec["listen"].(string)
	if rec["data_dir"] != dir {
		t.Errorf("data_dir = %v, want %q", rec["data_dir"], dir)
	}

	resp, err := http.Get("http://" + addr + "/bucket/key")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), "<Code>NoSuchBucket</Code>") {
		t.Errorf("GET /bucket/key = %d %q, want 404 NoSuchBucket", resp.StatusCode, raw)
	}

	r.cancel()
	if c := r.code(t); c != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", c, r.stderr.String())
	}
}

func TestMainCreatesBucket(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := launch(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return Main(ctx, []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, stdout, stderr, fsx.OS{})
	})
	addr, _ := r.waitFor(t, "listening")["listen"].(string)

	for _, want := range []int{http.StatusOK, http.StatusConflict} {
		req, err := http.NewRequest(http.MethodPut, "http://"+addr+"/demo", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("PUT /demo = %d, want %d", resp.StatusCode, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "buckets", "demo.json")); err != nil {
		t.Fatalf("bucket record: %v", err)
	}

	r.cancel()
	if c := r.code(t); c != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", c, r.stderr.String())
	}
}

// TestMainObjectOpsNeedBucket drives every object operation through Main with
// a real store: NoSuchBucket on a missing bucket, and once the bucket exists
// the operation itself (NotImplemented for those not built yet).
func TestMainObjectOpsNeedBucket(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := launch(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return Main(ctx, []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, stdout, stderr, fsx.OS{})
	})
	base := "http://" + r.waitFor(t, "listening")["listen"].(string)

	send := func(method, path string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(""))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, string(raw)
	}
	objectOps := []struct{ method, path string }{
		{http.MethodPut, "/demo/k"},
		{http.MethodGet, "/demo/k"},
		{http.MethodHead, "/demo/k"},
		{http.MethodDelete, "/demo/k"},
		{http.MethodGet, "/demo?list-type=2"},
	}
	for _, op := range objectOps {
		status, body := send(op.method, op.path)
		want := op.method == http.MethodHead || strings.Contains(body, "<Code>NoSuchBucket</Code>")
		if status != http.StatusNotFound || !want {
			t.Fatalf("%s %s on a missing bucket = %d %q, want 404 NoSuchBucket", op.method, op.path, status, body)
		}
	}
	if status, _ := send(http.MethodPut, "/demo"); status != http.StatusOK {
		t.Fatalf("create = %d", status)
	}
	// Implemented operations answer for real (the PUT creates demo/k, so
	// GET finds it); the rest answer NotImplemented until their feature.
	implemented := map[string]int{
		http.MethodPut:    http.StatusOK,
		http.MethodGet:    http.StatusOK,
		http.MethodHead:   http.StatusOK,
		http.MethodDelete: http.StatusNoContent,
	}
	for _, op := range objectOps {
		want, ok := implemented[op.method]
		if !ok {
			want = http.StatusNotImplemented
		}
		if status, _ := send(op.method, op.path); status != want {
			t.Fatalf("%s %s on an existing bucket = %d, want %d", op.method, op.path, status, want)
		}
	}

	r.cancel()
	if c := r.code(t); c != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", c, r.stderr.String())
	}
}

func TestMainPutObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := launch(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return Main(ctx, []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, stdout, stderr, fsx.OS{})
	})
	base := "http://" + r.waitFor(t, "listening")["listen"].(string)

	send := func(path, body string, header http.Header) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range header {
			req.Header[k] = v
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp
	}
	if resp := send("/demo", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	header := http.Header{"Content-Type": {"text/plain"}, "X-Amz-Meta-Owner": {"alice"}, "X-Amz-Meta-Team": {"core"}}
	resp := send("/demo/a/b.txt", "hello world", header)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") != `"5eb63bbbe01eeed093cb22bb8f5acdc3"` {
		t.Fatalf("put = %d, ETag %q", resp.StatusCode, resp.Header.Get("ETag"))
	}
	r.cancel()
	if c := r.code(t); c != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", c, r.stderr.String())
	}

	store, err := storage.Open(fsx.OS{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.Head("demo", "a/b.txt")
	if err != nil || info.Size != 11 || info.MD5 != "5eb63bbbe01eeed093cb22bb8f5acdc3" {
		t.Fatalf("Head = %+v, %v", info, err)
	}
	if info.ContentType != "text/plain" || len(info.Metadata) != 2 || info.Metadata["owner"] != "alice" || info.Metadata["team"] != "core" {
		t.Fatalf("stored type %q, metadata %v", info.ContentType, info.Metadata)
	}
}

func TestMainStorageOpenFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fsys := fsx.NewFaulty(fsx.OS{}, fsx.Fault{Op: fsx.OpMkdirAll, PathPattern: filepath.Join(dir, "buckets"), Err: errors.New("injected")})
	var stdout, stderr bytes.Buffer
	c := Main(context.Background(), []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, &stdout, &stderr, fsys)
	if c != 1 {
		t.Fatalf("exit = %d, want 1", c)
	}
	if want := "s3d: storage: "; !strings.HasPrefix(stderr.String(), want) || !strings.Contains(stderr.String(), "injected") {
		t.Fatalf("stderr = %q, want prefix %q and the injected error", stderr.String(), want)
	}
	if strings.Contains(stdout.String(), "listening") {
		t.Fatalf("stdout reports listening: %q", stdout.String())
	}
}

func TestMainUsageError(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if c := Main(context.Background(), []string{"-bogus"}, &stdout, &stderr, fsx.OS{}); c != 2 {
		t.Fatalf("exit = %d, want 2", c)
	}
	if !strings.Contains(stderr.String(), "Usage of s3d") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestMainHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if c := Main(context.Background(), []string{"-h"}, &stdout, &stderr, fsx.OS{}); c != 0 {
		t.Fatalf("exit = %d, want 0", c)
	}
}

func TestMainListenFailure(t *testing.T) {
	t.Parallel()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	addr := held.Addr().String()

	var stdout, stderr bytes.Buffer
	c := Main(context.Background(), []string{"-listen=" + addr, "-data-dir=" + t.TempDir()}, &stdout, &stderr, fsx.OS{})
	if c != 1 {
		t.Fatalf("exit = %d, want 1", c)
	}
	if want := "s3d: listen " + addr + ": "; !strings.HasPrefix(stderr.String(), want) {
		t.Fatalf("stderr = %q, want prefix %q", stderr.String(), want)
	}
	if strings.Contains(stdout.String(), "listening") {
		t.Fatalf("stdout reports listening: %q", stdout.String())
	}
}

func TestRunForcedShutdown(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	cfg := config.Config{Listen: "127.0.0.1:0", DataDir: t.TempDir(), ShutdownTimeout: 50 * time.Millisecond}
	r := launch(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return run(ctx, cfg, stdout, stderr, fsx.OS{}, func(*slog.Logger, *storage.Store) http.Handler { return h })
	})
	addr, _ := r.waitFor(t, "listening")["listen"].(string)

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := http.Get("http://" + addr + "/stuck")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	t.Cleanup(func() {
		close(release)
		<-clientDone
	})

	<-started
	r.cancel()
	if c := r.code(t); c != 1 {
		t.Fatalf("exit = %d, want 1", c)
	}
	if !strings.Contains(r.stderr.String(), "forced") {
		t.Fatalf("stderr = %q, want forced shutdown message", r.stderr.String())
	}
}
