package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/config"
	"github.com/hermeto/tiny-s3/internal/fsx"
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
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", resp.StatusCode)
	}

	r.cancel()
	if c := r.code(t); c != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", c, r.stderr.String())
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
		return run(ctx, cfg, stdout, stderr, fsx.OS{}, h)
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
