//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// objectFields is the part of get-object and head-object output we check.
type objectFields struct {
	ETag          string
	ContentLength int64
	ContentType   string
	LastModified  string
	AcceptRanges  string
	Metadata      map[string]string
}

func decodeFields(t *testing.T, stdout string) objectFields {
	t.Helper()
	var f objectFields
	if err := json.Unmarshal([]byte(stdout), &f); err != nil {
		t.Fatalf("output %q: %v", stdout, err)
	}
	if _, err := time.Parse(time.RFC3339, f.LastModified); err != nil {
		t.Fatalf("LastModified %q: %v", f.LastModified, err)
	}
	return f
}

func TestGetObjectCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")

	files := map[string][]byte{
		"empty":     {},
		"small":     []byte("hello world"),
		"dir/10MiB": randomBytes(t, 10<<20),
	}
	for key, content := range files {
		putETag(t, p.addr, "--bucket", "demo", "--key", key, "--body", writeFile(t, content),
			"--content-type", "application/x-demo", "--metadata", "owner=alice")
		out := filepath.Join(t.TempDir(), "out")
		res := mustSucceed(t, p.addr, "get-object", "--bucket", "demo", "--key", key, out)
		got, err := os.ReadFile(out)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("%s: downloaded %d bytes (%v), want %d identical bytes", key, len(got), err, len(content))
		}
		f := decodeFields(t, res.stdout)
		if f.ETag != quotedMD5(content) || f.ContentLength != int64(len(content)) || f.ContentType != "application/x-demo" ||
			f.AcceptRanges != "bytes" || f.Metadata["owner"] != "alice" {
			t.Fatalf("%s: get-object fields %+v", key, f)
		}
	}
	mustFail(t, p.addr, "(NoSuchKey)", "get-object", "--bucket", "demo", "--key", "missing", filepath.Join(t.TempDir(), "out"))
	p.stop(t)
}

func TestHeadObjectCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	putETag(t, p.addr, "--bucket", "demo", "--key", "k", "--body", writeFile(t, []byte("hello world")),
		"--content-type", "text/plain", "--metadata", "owner=alice")
	get := decodeFields(t, mustSucceed(t, p.addr, "get-object", "--bucket", "demo", "--key", "k", filepath.Join(t.TempDir(), "out")).stdout)
	head := decodeFields(t, mustSucceed(t, p.addr, "head-object", "--bucket", "demo", "--key", "k").stdout)
	if head.ETag != get.ETag || head.ContentLength != get.ContentLength || head.ContentType != get.ContentType ||
		head.LastModified != get.LastModified || head.AcceptRanges != get.AcceptRanges || head.Metadata["owner"] != "alice" {
		t.Fatalf("head-object %+v, get-object %+v", head, get)
	}
	mustFail(t, p.addr, "(404)", "head-object", "--bucket", "demo", "--key", "missing")
	p.stop(t)
}

func TestConditionalCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	etag := putETag(t, p.addr, "--bucket", "demo", "--key", "k", "--body", writeFile(t, []byte("hello world")))
	out := filepath.Join(t.TempDir(), "out")
	get := []string{"get-object", "--bucket", "demo", "--key", "k"}
	mustFail(t, p.addr, "(304)", append(get, "--if-none-match", etag, out)...)
	mustFail(t, p.addr, "(PreconditionFailed)", append(get, "--if-match", `"0000"`, out)...)
	mustFail(t, p.addr, "(304)", append(get, "--if-modified-since", "2099-01-01T00:00:00Z", out)...)
	mustSucceed(t, p.addr, append(get, "--if-match", etag, out)...)
	p.stop(t)
}
