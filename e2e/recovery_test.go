//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// dirEntries lists dir, failing the test on error.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, de := range des {
		names = append(names, de.Name())
	}
	return names
}

// wantRecovery checks the counts of the recovery line s3d logged at startup.
func wantRecovery(t *testing.T, p *proc, want map[string]float64) {
	t.Helper()
	rec := p.seenRecord(t, "recovery")
	for k, v := range want {
		if rec[k] != v {
			t.Fatalf("recovery %s = %v, want %v (line %v)", k, rec[k], v, rec)
		}
	}
}

// objectRef is what a listing and a record agree on.
type objectRef struct {
	Size int64
	ETag string
}

// scanMeta decodes every record in dir/meta and returns those of bucket demo.
func scanMeta(t *testing.T, dir string) map[string]objectRef {
	t.Helper()
	out := map[string]objectRef{}
	for _, name := range dirEntries(t, filepath.Join(dir, "meta")) {
		raw, err := os.ReadFile(filepath.Join(dir, "meta", name))
		if err != nil {
			t.Fatal(err)
		}
		var rec struct {
			Bucket, Key string
			Size        int64
			MD5         string
		}
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
		if rec.Bucket == "demo" {
			out[rec.Key] = objectRef{Size: rec.Size, ETag: `"` + rec.MD5 + `"`}
		}
	}
	return out
}

func listedObjects(l listing) map[string]objectRef {
	out := map[string]objectRef{}
	for _, c := range l.Contents {
		out[c.Key] = objectRef{Size: c.Size, ETag: c.ETag}
	}
	return out
}

// startUpload PUTs a body of size bytes to demo/key through a pipe and
// returns the pipe writer; the upload's result arrives on done.
func startUpload(t *testing.T, addr, key string, size int64, done chan<- error) *io.PipeWriter {
	t.Helper()
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPut, "http://"+addr+"/demo/"+key, pr)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = size
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("status %d", resp.StatusCode)
			}
		}
		done <- err
	}()
	return pw
}

// TestKillDuringUploads sends SIGKILL while ten of twenty parallel uploads
// are mid-body, then restarts on the same data directory.
func TestKillDuringUploads(t *testing.T) {
	const size = 4 << 20
	dir := t.TempDir()
	p := startProcIn(t, dir)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")

	bodies := make([][]byte, 20)
	for i := range bodies {
		bodies[i] = randomBytes(t, size)
	}
	acked := make(chan error, 10)
	stalled := make(chan error, 10)
	var writers sync.WaitGroup
	release := make(chan struct{})
	for i := range 20 {
		key := fmt.Sprintf("key-%02d", i)
		if i < 10 {
			pw := startUpload(t, p.addr, key, size, acked)
			writers.Add(1)
			go func() {
				defer writers.Done()
				_, _ = pw.Write(bodies[i])
				_ = pw.Close()
			}()
			continue
		}
		pw := startUpload(t, p.addr, key, size, stalled)
		writers.Add(1)
		go func() {
			defer writers.Done()
			_, _ = pw.Write(bodies[i][:1<<20])
			<-release
			_ = pw.CloseWithError(errors.New("client stopped"))
		}()
	}
	for range 10 {
		select {
		case err := <-acked:
			if err != nil {
				t.Fatalf("complete upload: %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Fatal("complete uploads not acknowledged within 60s")
		}
	}
	// The kill must land while the ten stalled uploads are in flight: wait
	// until each holds its temp file.
	deadline := time.Now().Add(30 * time.Second)
	for len(dirEntries(t, filepath.Join(dir, "tmp"))) != 10 {
		if time.Now().After(deadline) {
			t.Fatalf("tmp = %q, want the 10 in-flight uploads", dirEntries(t, filepath.Join(dir, "tmp")))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "planted-orphan"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p.exit(t, time.Now())
	close(release)
	writers.Wait()
	for range 10 {
		if err := <-stalled; err == nil {
			t.Fatal("a stalled upload was acknowledged")
		}
	}

	p = startProcIn(t, dir)
	wantRecovery(t, p, map[string]float64{"tmp_removed": 10, "orphans_removed": 1, "objects_indexed": 10, "records_skipped": 0, "buckets": 1})
	if got := dirEntries(t, filepath.Join(dir, "tmp")); len(got) != 0 {
		t.Fatalf("tmp after restart = %q", got)
	}
	if got := dirEntries(t, filepath.Join(dir, "data")); len(got) != 10 {
		t.Fatalf("data after restart holds %d files, want 10", len(got))
	}

	l := listObjects(t, p.addr)
	var want []string
	for i := range 10 {
		want = append(want, fmt.Sprintf("key-%02d", i))
	}
	if !slices.Equal(l.keys(), want) {
		t.Fatalf("listed %q, want the 10 acknowledged keys", l.keys())
	}
	// The listing equals a scan of the records on disk.
	if got, want := listedObjects(l), scanMeta(t, dir); !maps.Equal(got, want) {
		t.Fatalf("listing %v, records on disk %v", got, want)
	}
	for i, c := range l.Contents {
		out := filepath.Join(t.TempDir(), "out")
		mustSucceed(t, p.addr, "get-object", "--bucket", "demo", "--key", c.Key, out)
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, bodies[i]) || quotedMD5(got) != c.ETag {
			t.Fatalf("%s: %d bytes, ETag %s; want the uploaded body and a matching ETag", c.Key, len(got), c.ETag)
		}
	}
	p.stop(t)
}

// TestConcurrentPutsOneKey sends 50 uploads of distinct bodies to one key at
// once: the key must hold exactly one of them, with no body left behind.
func TestConcurrentPutsOneKey(t *testing.T) {
	dir := t.TempDir()
	p := startProcIn(t, dir)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")

	bodies := make([][]byte, 50)
	for i := range bodies {
		bodies[i] = randomBytes(t, 64<<10)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			httpPut(t, p.addr, "contended", string(bodies[i]))
		}()
	}
	close(start)
	wg.Wait()

	out := filepath.Join(t.TempDir(), "out")
	res := mustSucceed(t, p.addr, "get-object", "--bucket", "demo", "--key", "contended", out)
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(bodies, func(b []byte) bool { return bytes.Equal(b, got) }) {
		t.Fatal("the key holds none of the uploaded bodies")
	}
	if f := decodeFields(t, res.stdout); f.ETag != quotedMD5(got) {
		t.Fatalf("ETag %s does not match the bytes", f.ETag)
	}
	if n := len(dirEntries(t, filepath.Join(dir, "data"))); n != 1 {
		t.Fatalf("data holds %d bodies, want 1", n)
	}
	if n := len(dirEntries(t, filepath.Join(dir, "tmp"))); n != 0 {
		t.Fatalf("tmp holds %d files, want 0", n)
	}
	p.stop(t)
}
