//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// listing is the part of list-objects-v2 output the tests check.
type listing struct {
	Contents []struct {
		Key          string
		Size         int64
		ETag         string
		LastModified string
		StorageClass string
	}
	CommonPrefixes        []struct{ Prefix string }
	KeyCount              int
	IsTruncated           bool
	NextContinuationToken string
}

func (l listing) keys() []string {
	out := []string{}
	for _, c := range l.Contents {
		out = append(out, c.Key)
	}
	return out
}

func (l listing) prefixes() []string {
	out := []string{}
	for _, p := range l.CommonPrefixes {
		out = append(out, p.Prefix)
	}
	return out
}

// listObjects runs list-objects-v2 on bucket demo with extra arguments.
func listObjects(t *testing.T, addr string, args ...string) listing {
	t.Helper()
	res := mustSucceed(t, addr, append([]string{"list-objects-v2", "--bucket", "demo"}, args...)...)
	var l listing
	if strings.TrimSpace(res.stdout) == "" {
		return l // the CLI prints nothing for an empty bucket
	}
	if err := json.Unmarshal([]byte(res.stdout), &l); err != nil {
		t.Fatalf("list-objects-v2 output %q: %v", res.stdout, err)
	}
	return l
}

// putKeys uploads each key with its own name as the body.
func putKeys(t *testing.T, addr string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		putETag(t, addr, "--bucket", "demo", "--key", k, "--body", writeFile(t, []byte(k)))
	}
}

func TestListObjectsCLI(t *testing.T) {
	dir := t.TempDir()
	p := startProcIn(t, dir)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	if l := listObjects(t, p.addr); len(l.keys()) != 0 {
		t.Fatalf("empty bucket lists %q", l.keys())
	}

	keys := []string{"top", "dir/f1", "dir/sub/f2", "a", "é", "Z", "dir/sub/f3"}
	shuffled := slices.Clone(keys)
	rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	putKeys(t, p.addr, shuffled...)
	slices.Sort(keys) // Go compares strings by bytes: UTF-8 binary order

	// --no-paginate shows the server's own response: the CLI's paginator
	// merges pages and drops per-page fields such as KeyCount.
	l := listObjects(t, p.addr, "--no-paginate")
	if !slices.Equal(l.keys(), keys) || l.KeyCount != len(keys) || l.IsTruncated {
		t.Fatalf("listed %q (KeyCount %d, truncated %v), want %q", l.keys(), l.KeyCount, l.IsTruncated, keys)
	}
	for _, c := range l.Contents {
		if c.Size != int64(len(c.Key)) || c.ETag != quotedMD5([]byte(c.Key)) || c.StorageClass != "STANDARD" {
			t.Fatalf("entry %+v", c)
		}
		if _, err := time.Parse(time.RFC3339, c.LastModified); err != nil {
			t.Fatalf("LastModified %q: %v", c.LastModified, err)
		}
	}

	// Acknowledged writes and deletes show at once.
	putKeys(t, p.addr, "new")
	mustSucceed(t, p.addr, "delete-object", "--bucket", "demo", "--key", "top")
	got := listObjects(t, p.addr).keys()
	if !slices.Contains(got, "new") || slices.Contains(got, "top") || len(got) != len(keys) {
		t.Fatalf("after put and delete: %q", got)
	}

	// The listing is rebuilt after a restart.
	p.stop(t)
	p = startProcIn(t, dir)
	if after := listObjects(t, p.addr).keys(); !slices.Equal(after, got) {
		t.Fatalf("after restart %q, before %q", after, got)
	}
	mustFail(t, p.addr, "(NoSuchBucket)", "list-objects-v2", "--bucket", "missing")
	p.stop(t)
}

func TestListPrefixesCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	putKeys(t, p.addr, "dir/f1", "dir/sub/f2", "dir/sub/f3", "top")

	l := listObjects(t, p.addr, "--no-paginate", "--prefix", "dir/", "--delimiter", "/")
	if !slices.Equal(l.keys(), []string{"dir/f1"}) || !slices.Equal(l.prefixes(), []string{"dir/sub/"}) || l.KeyCount != 2 {
		t.Fatalf("prefix dir/ delimiter /: keys %q, prefixes %q, KeyCount %d", l.keys(), l.prefixes(), l.KeyCount)
	}
	if l := listObjects(t, p.addr, "--prefix", "dir/sub/"); !slices.Equal(l.keys(), []string{"dir/sub/f2", "dir/sub/f3"}) {
		t.Fatalf("prefix dir/sub/: %q", l.keys())
	}

	// aws s3 ls, a different CLI command on the same API.
	path, _ := exec.LookPath("aws")
	cmd := exec.Command(path, "--endpoint-url", "http://"+p.addr, "s3", "ls", "s3://demo/dir/")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test", "AWS_DEFAULT_REGION=us-east-1", "AWS_EC2_METADATA_DISABLED=true", "AWS_MAX_ATTEMPTS=1"}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "PRE sub/") || !strings.Contains(string(out), " f1\n") {
		t.Fatalf("aws s3 ls: %v\n%s", err, out)
	}
	p.stop(t)
}

// TestListSpecialKeysCLI lists keys that URL encoding changes; the CLI asks
// for encoding-type=url by default and decodes the answer.
func TestListSpecialKeysCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	keys := []string{"sp ace/a b", "plus+sign", "100%", "émoji-ü", "q?x=1&y#z", "tab\there", "a/./b", "~tilde"}
	putKeys(t, p.addr, keys...)
	slices.Sort(keys)
	if got := listObjects(t, p.addr).keys(); !slices.Equal(got, keys) {
		t.Fatalf("listed %q, want %q", got, keys)
	}
	if l := listObjects(t, p.addr, "--no-paginate", "--prefix", "sp ace/", "--delimiter", " "); !slices.Equal(l.keys(), []string{}) ||
		!slices.Equal(l.prefixes(), []string{"sp ace/a "}) {
		t.Fatalf("space delimiter: keys %q, prefixes %q", l.keys(), l.prefixes())
	}
	p.stop(t)
}

// httpPut stores body under demo/key with a plain HTTP client.
func httpPut(t *testing.T, addr, key, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://"+addr+"/demo/"+key, strings.NewReader(body))
	if err != nil {
		t.Error(err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("PUT %s: %d", key, resp.StatusCode)
	}
}

// TestListPagingUnderWritesCLI pages through 2,500 keys with the CLI, one
// page per call, while another client adds keys across the key space and
// overwrites original keys. Every original key must appear exactly once.
func TestListPagingUnderWritesCLI(t *testing.T) {
	p := startProc(t)
	mustSucceed(t, p.addr, "create-bucket", "--bucket", "demo")
	var originals []string
	for i := range 2500 {
		k := fmt.Sprintf("k%04d", i*2)
		originals = append(originals, k)
		httpPut(t, p.addr, k, k)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			httpPut(t, p.addr, fmt.Sprintf("k%04d", (i*37)%5000|1), "new") // odd: never an original
			httpPut(t, p.addr, originals[(i*53)%len(originals)], "overwritten")
		}
	}()

	seen := map[string]int{}
	args := []string{"--no-paginate", "--max-keys", "100"}
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("paging does not end")
		}
		l := listObjects(t, p.addr, args...)
		for _, k := range l.keys() {
			seen[k]++
		}
		if !l.IsTruncated {
			break
		}
		args = []string{"--no-paginate", "--max-keys", "100", "--continuation-token", l.NextContinuationToken}
	}
	close(stop)
	wg.Wait()
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("%s listed %d times", k, n)
		}
	}
	for _, k := range originals {
		if seen[k] != 1 {
			t.Fatalf("original %s listed %d times", k, seen[k])
		}
	}

	// The CLI's own paginator follows the tokens itself.
	all := listObjects(t, p.addr, "--page-size", "100").keys()
	if len(all) < len(originals) || !slices.IsSorted(all) {
		t.Fatalf("paginated listing: %d keys, sorted %v", len(all), slices.IsSorted(all))
	}
	if l := listObjects(t, p.addr, "--no-paginate", "--start-after", "k4996"); !slices.Contains(l.keys(), "k4998") || slices.Contains(l.keys(), "k4996") {
		t.Fatalf("start-after: %q", l.keys())
	}
	p.stop(t)
}
