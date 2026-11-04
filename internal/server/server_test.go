package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/server"
)

// syncBuffer is a bytes.Buffer safe for the concurrent writes slog makes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records returns the decoded JSON log lines.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func (b *syncBuffer) find(t *testing.T, msg string) map[string]any {
	t.Helper()
	for _, rec := range b.records(t) {
		if rec["msg"] == msg {
			return rec
		}
	}
	t.Fatalf("no %q log record in %v", msg, b.records(t))
	return nil
}

type harness struct {
	addr   string
	logs   *syncBuffer
	cancel context.CancelCauseFunc
	done   chan error
}

func start(t *testing.T, h http.Handler, shutdownTimeout time.Duration) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })

	hs := &harness{addr: ln.Addr().String(), logs: &syncBuffer{}, cancel: cancel, done: make(chan error, 1)}
	logger := slog.New(slog.NewJSONHandler(hs.logs, nil))
	go func() { hs.done <- server.Run(ctx, ln, h, shutdownTimeout, logger) }()
	return hs
}

func (hs *harness) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-hs.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s")
		return nil
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
}

func get(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRunIdleShutdown(t *testing.T) {
	t.Parallel()
	hs := start(t, okHandler(), 5*time.Second)

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	if got := get(t, client, "http://"+hs.addr+"/"); got != "ok" {
		t.Fatalf("body = %q", got)
	}

	hs.cancel(errors.New("test stop"))
	if err := hs.wait(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if got := hs.logs.find(t, "shutdown_started")["cause"]; got != "test stop" {
		t.Errorf("cause = %v, want %q", got, "test stop")
	}
	if _, ok := hs.logs.find(t, "shutdown_complete")["elapsed"]; !ok {
		t.Error("shutdown_complete has no elapsed attribute")
	}
}

func TestRunDrainsInFlightRequest(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})
	hs := start(t, h, 5*time.Second)

	type result struct {
		status int
		body   string
		err    error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + hs.addr + "/slow")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		resCh <- result{status: resp.StatusCode, body: string(body), err: err}
	}()

	<-started
	hs.cancel(errors.New("test stop"))

	// The listener closes asynchronously inside Shutdown, so poll. Close
	// every probe that connects: Shutdown waits for new connections younger
	// than 5s, and an open probe would stall the drain.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.Dial("tcp", hs.addr)
		if err != nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatal("listener still accepting 2s after cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	res := <-resCh
	if res.err != nil || res.status != http.StatusOK || res.body != "done" {
		t.Fatalf("in-flight request = %+v, want 200 done", res)
	}
	if err := hs.wait(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

func TestRunIdleKeepAliveDoesNotBlock(t *testing.T) {
	t.Parallel()
	hs := start(t, okHandler(), 5*time.Second)

	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	client := &http.Client{Transport: tr}
	get(t, client, "http://"+hs.addr+"/")

	began := time.Now()
	hs.cancel(errors.New("test stop"))
	if err := hs.wait(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Fatalf("idle keep-alive connection held shutdown for %v", elapsed)
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
	hs := start(t, h, 50*time.Millisecond)

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, err := http.Get("http://" + hs.addr + "/stuck")
		if err == nil {
			resp.Body.Close()
		}
	}()
	t.Cleanup(func() {
		close(release)
		<-clientDone
	})

	<-started
	hs.cancel(errors.New("test stop"))
	err := hs.wait(t)
	if !errors.Is(err, server.ErrForcedShutdown) {
		t.Fatalf("Run = %v, want ErrForcedShutdown", err)
	}
	if _, ok := hs.logs.find(t, "shutdown_forced")["elapsed"]; !ok {
		t.Error("shutdown_forced has no elapsed attribute")
	}
}

func TestRunServeError(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	err = server.Run(context.Background(), ln, okHandler(), time.Second, logger)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Run = %v, want a serve error", err)
	}
}
