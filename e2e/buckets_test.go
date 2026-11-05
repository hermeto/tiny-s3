//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cliResult is one finished `aws s3api` call.
type cliResult struct {
	stdout, stderr string
	code           int
}

// awsCLI runs the real `aws s3api` against addr with throwaway credentials,
// an empty temporary HOME and no retries, and returns its output. The e2e
// image provides aws (make e2e); its absence fails the test, never skips it.
func awsCLI(t *testing.T, addr string, args ...string) cliResult {
	t.Helper()
	path, err := exec.LookPath("aws")
	if err != nil {
		t.Fatalf("aws CLI not found (run the e2e tests with make e2e): %v", err)
	}
	home := t.TempDir()
	cmd := exec.Command(path, append([]string{"--endpoint-url", "http://" + addr, "--output", "json", "s3api"}, args...)...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"AWS_CONFIG_FILE=" + filepath.Join(home, "config"),
		"AWS_SHARED_CREDENTIALS_FILE=" + filepath.Join(home, "credentials"),
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_DEFAULT_REGION=us-east-1",
		"AWS_EC2_METADATA_DISABLED=true",
		"AWS_MAX_ATTEMPTS=1",
		"AWS_PAGER=",
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	res := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("aws %v: %v", args, err)
	}
	return res
}

// mustSucceed runs aws s3api and fails the test unless it exits 0.
func mustSucceed(t *testing.T, addr string, args ...string) cliResult {
	t.Helper()
	res := awsCLI(t, addr, args...)
	if res.code != 0 {
		t.Fatalf("aws s3api %v: exit %d, stderr %q", args, res.code, res.stderr)
	}
	return res
}

// mustFail runs aws s3api and fails the test unless it exits non-zero with
// want (an error code such as "(NoSuchBucket)") on stderr.
func mustFail(t *testing.T, addr, want string, args ...string) {
	t.Helper()
	res := awsCLI(t, addr, args...)
	if res.code == 0 || !strings.Contains(res.stderr, want) {
		t.Fatalf("aws s3api %v: exit %d, stderr %q, want failure with %q", args, res.code, res.stderr, want)
	}
}

// stop sends SIGTERM and waits for a clean exit.
func (p *proc) stop(t *testing.T) {
	t.Helper()
	p.signal(t, syscall.SIGTERM)
	if code, _ := p.exit(t, time.Now()); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, p.stderr.String())
	}
}

func TestCreateBucketCLI(t *testing.T) {
	p := startProc(t)

	res := mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	var out struct{ Location string }
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil || out.Location != "/demo" {
		t.Fatalf("create-bucket output %q (%v), want Location /demo", res.stdout, err)
	}
	mustFail(t, p.addr, "(BucketAlreadyOwnedByYou)", "create-bucket", "--bucket", "demo")
	mustFail(t, p.addr, "(InvalidBucketName)", "create-bucket", "--bucket", "Bad_Name")
	mustFail(t, p.addr, "(InvalidLocationConstraint)", "create-bucket", "--bucket", "eu-bucket",
		"--create-bucket-configuration", "LocationConstraint=eu-west-1")
	p.stop(t)
}

// listedBucket is one entry of `aws s3api list-buckets` output.
type listedBucket struct {
	Name         string
	CreationDate string
}

// listBuckets runs list-buckets and returns its buckets, checking that every
// CreationDate parses.
func listBuckets(t *testing.T, addr string) []listedBucket {
	t.Helper()
	res := mustSucceed(t, addr, "list-buckets")
	var out struct{ Buckets []listedBucket }
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		t.Fatalf("list-buckets output %q: %v", res.stdout, err)
	}
	for _, b := range out.Buckets {
		if _, err := time.Parse(time.RFC3339, b.CreationDate); err != nil {
			t.Fatalf("bucket %s: CreationDate %q: %v", b.Name, b.CreationDate, err)
		}
	}
	return out.Buckets
}

func TestBucketSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	p := startProcIn(t, dir)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	before := listBuckets(t, p.addr)
	p.stop(t)

	p = startProcIn(t, dir)
	mustSucceed(t, p.addr, "head-bucket", "--bucket", "demo")
	mustFail(t, p.addr, "(BucketAlreadyOwnedByYou)", "create-bucket", "--bucket", "demo")
	after := listBuckets(t, p.addr)
	if len(before) != 1 || len(after) != 1 || before[0] != after[0] {
		t.Fatalf("list-buckets before restart %v, after %v; want the same single bucket", before, after)
	}
	p.stop(t)
}

func TestListBucketsCLI(t *testing.T) {
	p := startProc(t)
	if got := listBuckets(t, p.addr); len(got) != 0 {
		t.Fatalf("list-buckets on a new server = %v, want none", got)
	}
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "beta")
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "alpha")
	got := listBuckets(t, p.addr)
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("list-buckets = %v, want alpha then beta", got)
	}
	p.stop(t)
}

func TestHeadBucketCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")

	res := mustSucceed(t, p.addr, "head-bucket", "--bucket", "demo")
	var out struct{ BucketRegion string }
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil || out.BucketRegion != "us-east-1" {
		t.Fatalf("head-bucket output %q (%v), want BucketRegion us-east-1", res.stdout, err)
	}
	mustFail(t, p.addr, "(404)", "head-bucket", "--bucket", "missing")
	mustFail(t, p.addr, "(400)", "head-bucket", "--bucket", "Bad_Name")
	p.stop(t)
}

func TestObjectOpsOnMissingBucketCLI(t *testing.T) {
	p := startProc(t)
	body := filepath.Join(t.TempDir(), "body")
	if err := os.WriteFile(body, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")

	mustFail(t, p.addr, "(NoSuchBucket)", "put-object", "--bucket", "missing", "--key", "k", "--body", body)
	mustFail(t, p.addr, "(NoSuchBucket)", "get-object", "--bucket", "missing", "--key", "k", out)
	mustFail(t, p.addr, "(NoSuchBucket)", "delete-object", "--bucket", "missing", "--key", "k")
	mustFail(t, p.addr, "(NoSuchBucket)", "list-objects-v2", "--bucket", "missing")
	mustFail(t, p.addr, "(404)", "head-object", "--bucket", "missing", "--key", "k")

	// Once the bucket exists the gate lets requests through; GetObject
	// itself comes with a later feature.
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	mustFail(t, p.addr, "(NotImplemented)", "get-object", "--bucket", "demo", "--key", "k", out)
	p.stop(t)
}
