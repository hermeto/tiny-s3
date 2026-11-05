//go:build e2e

// Package e2e drives the real s3d binary: it is built once in TestMain,
// started with os/exec, and stopped with real signals.
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "s3d-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "s3d")
	build := exec.Command("go", "build", "-o", binary, "../cmd/s3d")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build s3d:", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

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

type proc struct {
	cmd    *exec.Cmd
	addr   string
	stderr *syncBuffer
	lines  chan map[string]any // decoded stdout records
	done   chan int            // exit code
}

func startProc(t *testing.T) *proc {
	t.Helper()
	return startProcIn(t, t.TempDir())
}

// startProcIn starts s3d on dataDir, so a test can restart it on the same data.
func startProcIn(t *testing.T, dataDir string) *proc {
	t.Helper()
	cmd := exec.Command(binary, "-listen=127.0.0.1:0", "-data-dir="+dataDir)
	p := &proc{cmd: cmd, stderr: &syncBuffer{}, lines: make(chan map[string]any, 64), done: make(chan int, 1)}
	cmd.Stderr = p.stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	go func() {
		defer close(p.lines)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var rec map[string]any
			if json.Unmarshal(sc.Bytes(), &rec) == nil {
				p.lines <- rec
			}
		}
	}()
	rec := p.waitFor(t, "listening")
	p.addr, _ = rec["listen"].(string)
	go func() {
		err := cmd.Wait()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
			p.done <- 0
		case errors.As(err, &exitErr):
			p.done <- exitErr.ExitCode()
		default:
			p.done <- -1
		}
	}()
	return p
}

func (p *proc) waitFor(t *testing.T, msg string) map[string]any {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case rec, ok := <-p.lines:
			if !ok {
				t.Fatalf("stdout closed before %q; stderr: %s", msg, p.stderr.String())
			}
			if rec["msg"] == msg {
				return rec
			}
		case <-timeout:
			t.Fatalf("no %q record within 10s; stderr: %s", msg, p.stderr.String())
		}
	}
}

func (p *proc) signal(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
}

// exit waits for the process and returns its exit code and the time since since.
func (p *proc) exit(t *testing.T, since time.Time) (int, time.Duration) {
	t.Helper()
	select {
	case code := <-p.done:
		return code, time.Since(since)
	case <-time.After(15 * time.Second):
		t.Fatal("process did not exit within 15s")
		return -1, 0
	}
}

// sendAndSettle opens a connection, writes raw, and waits briefly. No other
// sync point exists with an external process: the pause gives the server
// time to accept the connection and act on the bytes.
func sendAndSettle(t *testing.T, addr, raw string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, raw); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	return conn
}

// holdInFlightRequest sends a complete header and half of a small declared
// body. The bucket gate answers NoSuchBucket at once, but before flushing the
// response net/http drains the unread body (it is under 256 KiB), which
// blocks on this client while the connection is StateActive. Shutdown waits
// for it until the rest of the body arrives or the deadline passes.
func holdInFlightRequest(t *testing.T, addr string) net.Conn {
	t.Helper()
	return sendAndSettle(t, addr, "PUT /missing/k HTTP/1.1\r\nHost: x\r\nContent-Length: 10\r\n\r\n01234")
}

// holdPartialHeader sends a request header without its final blank line.
// net/http never treats that as in flight: it closes the connection as idle
// once it is 5 s old, and drops a header completed after Shutdown began.
func holdPartialHeader(t *testing.T, addr string) net.Conn {
	t.Helper()
	return sendAndSettle(t, addr, "GET /b/k HTTP/1.1\r\nHost: x\r\n")
}

func TestIdleStopSIGTERM(t *testing.T) {
	t.Parallel()
	for i := range 20 {
		p := startProc(t)
		began := time.Now()
		p.signal(t, syscall.SIGTERM)
		code, elapsed := p.exit(t, began)
		if code != 0 || elapsed >= time.Second {
			t.Fatalf("run %d: exit %d after %v, want 0 in under 1s; stderr: %s", i, code, elapsed, p.stderr.String())
		}
	}
}

func TestIdleStopSIGINT(t *testing.T) {
	t.Parallel()
	p := startProc(t)
	began := time.Now()
	p.signal(t, os.Interrupt)
	code, elapsed := p.exit(t, began)
	if code != 0 || elapsed >= time.Second {
		t.Fatalf("exit %d after %v, want 0 in under 1s", code, elapsed)
	}
}

func TestDrainWithSecondSignal(t *testing.T) {
	t.Parallel()
	p := startProc(t)
	conn := holdInFlightRequest(t, p.addr)

	began := time.Now()
	p.signal(t, syscall.SIGTERM)
	p.waitFor(t, "shutdown_started")
	p.signal(t, syscall.SIGTERM) // must be absorbed, not cut the drain short

	if _, err := io.WriteString(conn, "56789"); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "NoSuchBucket") {
		t.Fatalf("response = %d %q (%v), want a complete 404 NoSuchBucket", resp.StatusCode, body, err)
	}

	code, elapsed := p.exit(t, began)
	if code != 0 || elapsed >= 10*time.Second {
		t.Fatalf("exit %d after %v, want 0 within 10s", code, elapsed)
	}
}

func TestStalledHeaderDoesNotHoldShutdown(t *testing.T) {
	t.Parallel()
	p := startProc(t)
	conn := holdPartialHeader(t, p.addr)

	began := time.Now()
	p.signal(t, syscall.SIGTERM)

	// net/http treats a connection that has not finished its first header
	// for 5s as idle during Shutdown and closes it.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if n, err := conn.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read = %d, %v; want EOF from the server closing the connection", n, err)
	}

	code, elapsed := p.exit(t, began)
	if code != 0 || elapsed >= 10*time.Second {
		t.Fatalf("exit %d after %v, want 0 within 10s", code, elapsed)
	}
	p.waitFor(t, "shutdown_complete")
}

func TestForcedShutdown(t *testing.T) {
	t.Parallel()
	p := startProc(t)
	holdInFlightRequest(t, p.addr) // the rest of the body never arrives

	began := time.Now()
	p.signal(t, syscall.SIGTERM)

	code, elapsed := p.exit(t, began)
	if code != 1 || elapsed < 9*time.Second || elapsed >= 10*time.Second {
		t.Fatalf("exit %d after %v, want 1 between 9s and 10s; stderr: %s", code, elapsed, p.stderr.String())
	}
	p.waitFor(t, "shutdown_forced")
	if !strings.Contains(p.stderr.String(), "shutdown forced at deadline") {
		t.Fatalf("stderr = %q, want the forced-shutdown error", p.stderr.String())
	}
}

func TestDataDirIsAFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(binary, "-listen=127.0.0.1:0", "-data-dir="+path)
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	if !strings.Contains(stderr.String(), "not a directory") {
		t.Fatalf("stderr = %q, want 'not a directory'", stderr.String())
	}
}

func TestUsageError(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	cmd := exec.Command(binary, "-bogus")
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("err = %v, want exit 2", err)
	}
	if !strings.Contains(stderr.String(), "Usage of s3d") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}
