package s3api

import (
	"encoding/base64"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hermeto/tiny-s3/internal/storage"
)

// maxListKeys is the S3 default and cap for max-keys.
const maxListKeys = 1000

// ObjectLister is the part of the storage engine ListObjectsV2 uses.
type ObjectLister interface {
	List(bucket string, opts storage.ListOptions) storage.ListResult
}

// listBucketResult is the ListObjectsV2 response. Optional elements are
// omitted when they carry nothing, as S3 does.
type listBucketResult struct {
	XMLName               xml.Name       `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix"`
	Delimiter             string         `xml:"Delimiter,omitempty"`
	MaxKeys               int            `xml:"MaxKeys"`
	EncodingType          string         `xml:"EncodingType,omitempty"`
	KeyCount              int            `xml:"KeyCount"`
	IsTruncated           bool           `xml:"IsTruncated"`
	ContinuationToken     string         `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	StartAfter            string         `xml:"StartAfter,omitempty"`
	Contents              []listEntry    `xml:"Contents"`
	CommonPrefixes        []commonPrefix `xml:"CommonPrefixes"`
}

type commonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type listEntry struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// listRequest is a checked ListObjectsV2 query.
type listRequest struct {
	prefix, delimiter string
	startAfter, token string
	maxKeys           int
	urlEncoding       bool
	// after and afterGroup are where the page starts: the decoded token,
	// else start-after.
	after      string
	afterGroup bool
}

// parseListRequest checks the ListObjectsV2 query parameters.
func parseListRequest(q url.Values) (listRequest, bool) {
	req := listRequest{
		prefix: q.Get("prefix"), delimiter: q.Get("delimiter"),
		startAfter: q.Get("start-after"), token: q.Get("continuation-token"),
		maxKeys: maxListKeys,
	}
	req.after = req.startAfter
	if q.Has("continuation-token") {
		after, group, ok := decodeToken(req.token)
		if !ok {
			return listRequest{}, false
		}
		req.after, req.afterGroup = after, group
	}
	if q.Has("max-keys") {
		n, err := strconv.Atoi(q.Get("max-keys"))
		if err != nil || n < 0 {
			return listRequest{}, false
		}
		req.maxKeys = min(n, maxListKeys)
	}
	switch q.Get("encoding-type") {
	case "":
	case "url":
		req.urlEncoding = true
	default:
		return listRequest{}, false
	}
	return req, true
}

// ListObjectsV2Op returns the ListObjectsV2 operation served by store, from
// its in-memory index.
func ListObjectsV2Op(store ObjectLister, log *slog.Logger) Op {
	return func(w http.ResponseWriter, r *http.Request, t Target) {
		q, _ := url.ParseQuery(r.URL.RawQuery)
		req, ok := parseListRequest(q)
		if !ok {
			WriteError(w, r, ErrInvalidArgument)
			return
		}
		page := store.List(t.Bucket, storage.ListOptions{
			Prefix: req.prefix, Delimiter: req.delimiter,
			After: req.after, AfterGroup: req.afterGroup, MaxKeys: req.maxKeys,
		})
		// enc applies encoding-type=url to every key-like value.
		enc := func(v string) string { return v }
		if req.urlEncoding {
			enc = s3URLEncode
		}
		res := listBucketResult{
			Name:              t.Bucket,
			Prefix:            enc(req.prefix),
			Delimiter:         enc(req.delimiter),
			MaxKeys:           req.maxKeys,
			IsTruncated:       page.Truncated,
			ContinuationToken: req.token,
			StartAfter:        enc(req.startAfter),
		}
		if page.Truncated {
			res.NextContinuationToken = encodeToken(page.Next, page.NextGroup)
		}
		if req.urlEncoding {
			res.EncodingType = "url"
		}
		for _, o := range page.Objects {
			res.Contents = append(res.Contents, listEntry{
				Key:          enc(o.Key),
				LastModified: o.LastModified.UTC().Format(s3Time),
				ETag:         `"` + o.MD5 + `"`,
				Size:         o.Size,
				StorageClass: "STANDARD",
			})
		}
		for _, p := range page.CommonPrefixes {
			res.CommonPrefixes = append(res.CommonPrefixes, commonPrefix{Prefix: enc(p)})
		}
		res.KeyCount = len(res.Contents) + len(res.CommonPrefixes)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		// The status is sent; a write error means the client went away.
		_, _ = io.WriteString(w, xml.Header)
		_ = xml.NewEncoder(w).Encode(res)
	}
}

// s3URLEncode is the encoding of encoding-type=url: every byte except the
// unreserved characters (RFC 3986) and "/" becomes %XX. A space is %20 and
// "+" is %2B, so both URL decoders clients use (unquote and unquote_plus)
// get the original value back.
func s3URLEncode(v string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(v) {
		c := v[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~', c == '/':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// Continuation tokens encode where a page ended: "k" + key, or "g" + common
// prefix to resume after every key of that group. They hold no server
// state, so they stay valid across restarts.
func encodeToken(next string, group bool) string {
	kind := "k"
	if group {
		kind = "g"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(kind + next))
}

func decodeToken(tok string) (after string, group, ok bool) {
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(b) < 2 {
		return "", false, false
	}
	switch b[0] {
	case 'k':
		return string(b[1:]), false, true
	case 'g':
		return string(b[1:]), true, true
	}
	return "", false, false
}
