//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteObjectCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	putETag(t, p.addr, "--bucket", "demo", "--key", "k", "--body", writeFile(t, []byte("hello world")))
	mustSucceed(t, p.addr, "delete-object", "--bucket", "demo", "--key", "k")
	mustSucceed(t, p.addr, "delete-object", "--bucket", "demo", "--key", "k") // idempotent
	mustFail(t, p.addr, "(404)", "head-object", "--bucket", "demo", "--key", "k")
	mustFail(t, p.addr, "(NoSuchKey)", "get-object", "--bucket", "demo", "--key", "k", filepath.Join(t.TempDir(), "out"))
	mustFail(t, p.addr, "(NoSuchBucket)", "delete-object", "--bucket", "missing", "--key", "k")
	p.stop(t)
}

// TestDeleteSurvivesKill sends SIGKILL right after an acknowledged delete
// and restarts on the same data: the object must stay deleted.
func TestDeleteSurvivesKill(t *testing.T) {
	dir := t.TempDir()
	p := startProcIn(t, dir)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	putETag(t, p.addr, "--bucket", "demo", "--key", "k", "--body", writeFile(t, []byte("hello world")))
	mustSucceed(t, p.addr, "delete-object", "--bucket", "demo", "--key", "k")
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p.exit(t, time.Now())

	p = startProcIn(t, dir)
	mustFail(t, p.addr, "(404)", "head-object", "--bucket", "demo", "--key", "k")
	p.stop(t)
}
