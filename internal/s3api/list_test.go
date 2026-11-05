package s3api

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// listed is the decoded ListBucketResult a test checks.
type listed struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string
	Prefix                string
	Delimiter             *string
	MaxKeys               int
	EncodingType          *string
	KeyCount              int
	IsTruncated           bool
	ContinuationToken     *string
	NextContinuationToken *string
	StartAfter            *string
	Contents              []struct {
		Key          string
		LastModified string
		ETag         string
		Size         int64
		StorageClass string
	}
	CommonPrefixes []struct{ Prefix string }
}

func (l listed) keys() []string {
	var out []string
	for _, c := range l.Contents {
		out = append(out, c.Key)
	}
	return out
}

func (l listed) prefixes() []string {
	var out []string
	for _, p := range l.CommonPrefixes {
		out = append(out, p.Prefix)
	}
	return out
}

// listStore returns a handler with ListObjectsV2 over a fresh real store
// holding keys (body = key) in bucket demo.
func listStore(t *testing.T, keys ...string) (http.Handler, *storage.Store) {
	t.Helper()
	store, err := storage.Open(fsx.OS{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, err := store.Put(t.Context(), "demo", k, strings.NewReader(k), int64(len(k)), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(Ops{ListObjectsV2: ListObjectsV2Op(store, discardLogger())}, discardLogger()), store
}

// list runs GET /demo?list-type=2&<query> and decodes a 200 answer.
func list(t *testing.T, h http.Handler, query string) listed {
	t.Helper()
	w := serveReq(h, httptest.NewRequest(http.MethodGet, "/demo?list-type=2&"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list %q: %d %s", query, w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("Content-Type %q", ct)
	}
	raw := w.Body.Bytes()
	if !bytes.HasPrefix(raw, []byte(xml.Header)) || !bytes.Contains(raw, []byte(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)) {
		t.Fatalf("body %q", raw)
	}
	var l listed
	if err := xml.Unmarshal(raw, &l); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	return l
}

func TestListObjectsV2Basic(t *testing.T) {
	t.Parallel()
	h, store := listStore(t, "z", "a/b", "a")
	l := list(t, h, "")
	if l.Name != "demo" || l.Prefix != "" || l.MaxKeys != 1000 || l.KeyCount != 3 || l.IsTruncated ||
		l.Delimiter != nil || l.EncodingType != nil || l.ContinuationToken != nil || l.NextContinuationToken != nil || l.StartAfter != nil {
		t.Fatalf("result %+v", l)
	}
	if got := strings.Join(l.keys(), ","); got != "a,a/b,z" {
		t.Fatalf("keys %s", got)
	}
	for _, c := range l.Contents {
		info, err := store.Head("demo", c.Key)
		if err != nil {
			t.Fatal(err)
		}
		if c.ETag != `"`+info.MD5+`"` || c.Size != info.Size || c.StorageClass != "STANDARD" ||
			c.LastModified != info.LastModified.UTC().Format("2006-01-02T15:04:05.000Z") {
			t.Fatalf("entry %+v, Head %+v", c, info)
		}
		if _, err := time.Parse(time.RFC3339, c.LastModified); err != nil {
			t.Fatal(err)
		}
	}

	empty, _ := listStore(t)
	if l := list(t, empty, ""); l.KeyCount != 0 || len(l.Contents) != 0 || l.IsTruncated {
		t.Fatalf("empty bucket: %+v", l)
	}
	if l := list(t, h, "max-keys=0"); l.KeyCount != 0 || l.IsTruncated || l.NextContinuationToken != nil || l.MaxKeys != 0 {
		t.Fatalf("max-keys=0: %+v", l)
	}
}

func TestListObjectsV2InvalidArguments(t *testing.T) {
	t.Parallel()
	h, _ := listStore(t, "a")
	for _, q := range []string{"max-keys=-1", "max-keys=abc", "max-keys=", "encoding-type=xml"} {
		wantError(t, serveReq(h, httptest.NewRequest(http.MethodGet, "/demo?list-type=2&"+q, nil)), http.MethodGet, ErrInvalidArgument)
	}
}

func TestListObjectsV2PrefixDelimiter(t *testing.T) {
	t.Parallel()
	h, _ := listStore(t, "dir/f1", "dir/sub/f2", "dir/sub/f3", "top")
	l := list(t, h, "prefix=dir/&delimiter=/")
	if l.Prefix != "dir/" || l.Delimiter == nil || *l.Delimiter != "/" || l.KeyCount != 2 ||
		strings.Join(l.keys(), ",") != "dir/f1" || strings.Join(l.prefixes(), ",") != "dir/sub/" {
		t.Fatalf("result %+v", l)
	}
}

func TestS3URLEncode(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"plain/key-1._~": "plain/key-1._~",
		"a b+c":          "a%20b%2Bc",
		"100%":           "100%25",
		"é":              "%C3%A9",
		"q?x=1&y#z":      "q%3Fx%3D1%26y%23z",
		"\x01":           "%01",
	} {
		if got := s3URLEncode(in); got != want {
			t.Errorf("s3URLEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListObjectsV2URLEncoding(t *testing.T) {
	t.Parallel()
	h, _ := listStore(t, "d ir/a b+c.txt", "d ir/sub dir/x", "é")
	l := list(t, h, "encoding-type=url&prefix=d+ir/&delimiter=/")
	if l.EncodingType == nil || *l.EncodingType != "url" || l.Prefix != "d%20ir/" || *l.Delimiter != "/" {
		t.Fatalf("result %+v", l)
	}
	if strings.Join(l.keys(), ",") != "d%20ir/a%20b%2Bc.txt" || strings.Join(l.prefixes(), ",") != "d%20ir/sub%20dir/" {
		t.Fatalf("keys %q, prefixes %q", l.keys(), l.prefixes())
	}
	if l := list(t, h, "encoding-type=url&prefix=%C3%A9"); strings.Join(l.keys(), ",") != "%C3%A9" || l.Prefix != "%C3%A9" {
		t.Fatalf("UTF-8: %+v", l)
	}
	// Without encoding-type the values are sent as they are.
	if l := list(t, h, "prefix=d+ir/&delimiter=/"); l.Prefix != "d ir/" || strings.Join(l.keys(), ",") != "d ir/a b+c.txt" {
		t.Fatalf("plain: %+v", l)
	}
}

func TestListObjectsV2Tokens(t *testing.T) {
	t.Parallel()
	h, _ := listStore(t, "a", "b", "c", "d/1", "d/2", "e")
	l := list(t, h, "max-keys=2")
	if !l.IsTruncated || l.NextContinuationToken == nil || l.KeyCount != 2 || strings.Join(l.keys(), ",") != "a,b" {
		t.Fatalf("page 1: %+v", l)
	}
	tok := *l.NextContinuationToken
	l = list(t, h, "max-keys=2&continuation-token="+url.QueryEscape(tok))
	if strings.Join(l.keys(), ",") != "c,d/1" || l.ContinuationToken == nil || *l.ContinuationToken != tok {
		t.Fatalf("page 2: %+v", l)
	}

	// A page that ends on a common prefix resumes after the whole group.
	l = list(t, h, "delimiter=/&max-keys=4")
	if strings.Join(l.keys(), ",") != "a,b,c" || strings.Join(l.prefixes(), ",") != "d/" || !l.IsTruncated {
		t.Fatalf("group page: %+v", l)
	}
	l = list(t, h, "delimiter=/&max-keys=4&continuation-token="+url.QueryEscape(*l.NextContinuationToken))
	if strings.Join(l.keys(), ",") != "e" || len(l.prefixes()) != 0 || l.IsTruncated || l.NextContinuationToken != nil {
		t.Fatalf("after the group: %+v", l)
	}

	// start-after alone, echoed; the token wins over it, which is still echoed.
	l = list(t, h, "start-after=c")
	if strings.Join(l.keys(), ",") != "d/1,d/2,e" || l.StartAfter == nil || *l.StartAfter != "c" {
		t.Fatalf("start-after: %+v", l)
	}
	l = list(t, h, "start-after=c&max-keys=2&continuation-token="+url.QueryEscape(tok))
	if strings.Join(l.keys(), ",") != "c,d/1" || l.StartAfter == nil || *l.StartAfter != "c" {
		t.Fatalf("token and start-after: %+v", l)
	}
	if l := list(t, h, "encoding-type=url&start-after=a+b"); l.StartAfter == nil || *l.StartAfter != "a%20b" {
		t.Fatalf("encoded start-after: %+v", l)
	}

	// max-keys above the cap.
	if l := list(t, h, "max-keys=5000"); l.MaxKeys != 1000 || l.KeyCount != 6 {
		t.Fatalf("max-keys=5000: %+v", l)
	}

	for _, bad := range []string{"!!!", base64.RawURLEncoding.EncodeToString([]byte("x-key")), base64.RawURLEncoding.EncodeToString([]byte("k")), ""} {
		r := httptest.NewRequest(http.MethodGet, "/demo?list-type=2&continuation-token="+url.QueryEscape(bad), nil)
		wantError(t, serveReq(h, r), http.MethodGet, ErrInvalidArgument)
	}
}

// TestListObjectsV2ConcurrentPaging pages through 2,500 keys while other
// goroutines add keys across the key space, overwrite original keys and
// delete added ones. Every original key must appear exactly once.
func TestListObjectsV2ConcurrentPaging(t *testing.T) {
	t.Parallel()
	var originals []string
	for i := range 2500 {
		originals = append(originals, fmt.Sprintf("k%04d", i*2))
	}
	h, store := listStore(t, originals...)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				added := fmt.Sprintf("k%04d", (i*7+w*1301)%5000|1) // odd: never an original
				_, _ = store.Put(t.Context(), "demo", added, strings.NewReader("x"), 1, storage.PutOptions{})
				orig := originals[(i*13+w)%len(originals)]
				_, _ = store.Put(t.Context(), "demo", orig, strings.NewReader("yy"), 2, storage.PutOptions{})
				_ = store.Delete("demo", added)
			}
		}()
	}

	seen := map[string]int{}
	query := "max-keys=100"
	for pages := 0; ; pages++ {
		if pages > 200 {
			t.Fatal("paging does not end")
		}
		l := list(t, h, query)
		for _, k := range l.keys() {
			seen[k]++
		}
		if !l.IsTruncated {
			break
		}
		query = "max-keys=100&continuation-token=" + url.QueryEscape(*l.NextContinuationToken)
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
}
