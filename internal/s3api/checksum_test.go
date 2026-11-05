package s3api

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestChecksumDigests(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"x-amz-checksum-crc32":     "DUoRhQ==",
		"x-amz-checksum-crc32c":    "yZRlqg==",
		"x-amz-checksum-crc64nvme": "jSnVw/bqjr4=", // from the aws CLI capture
		"x-amz-checksum-sha1":      "Kq5sNclPz7QV2+lfQIuc6R7oRu0=",
		"x-amz-checksum-sha256":    "uU0nuZNNPgilLlLX2n2r+sSE7+N6U4DukIj3rOLvzek=",
	}
	if len(checksumAlgs) != len(want) {
		t.Fatalf("%d algorithms, want %d", len(checksumAlgs), len(want))
	}
	for _, alg := range checksumAlgs {
		h := alg.newHash()
		_, _ = io.WriteString(h, "hello world")
		if got := base64.StdEncoding.EncodeToString(h.Sum(nil)); got != want[alg.name] || h.Size() != alg.size {
			t.Errorf("%s = %s (size %d), want %s", alg.name, got, h.Size(), want[alg.name])
		}
	}
}

// verifyBody runs body through the checksum c and its Verify, with trailers
// as the decoder would report them.
func verifyBody(c *checksum, body string, trailers map[string]string) error {
	r, verify := c.wrap(strings.NewReader(body), func(name string) (string, bool) {
		v, ok := trailers[name]
		return v, ok
	})
	if _, err := io.ReadAll(r); err != nil {
		return err
	}
	return verify()
}

func TestRequestChecksum(t *testing.T) {
	t.Parallel()
	crc := "jSnVw/bqjr4="
	tests := map[string]struct {
		header   http.Header
		trailers map[string]string
		none     bool  // no checksum requested
		pre      bool  // InvalidRequest before the body
		err      error // from Verify
	}{
		"no checksum":          {header: http.Header{}, none: true},
		"unknown algorithm":    {header: http.Header{"X-Amz-Checksum-Foo": {"x"}}, none: true},
		"sdk algorithm only":   {header: http.Header{"X-Amz-Sdk-Checksum-Algorithm": {"CRC64NVME"}}, none: true},
		"header ok":            {header: http.Header{"X-Amz-Checksum-Crc64nvme": {crc}}},
		"header mismatch":      {header: http.Header{"X-Amz-Checksum-Crc64nvme": {"AAAAAAAAAAA="}}, err: errChecksumMismatch},
		"header bad base64":    {header: http.Header{"X-Amz-Checksum-Crc64nvme": {"!!"}}, pre: true},
		"header wrong length":  {header: http.Header{"X-Amz-Checksum-Crc64nvme": {"AAAAAA=="}}, pre: true},
		"two headers":          {header: http.Header{"X-Amz-Checksum-Crc32": {"DUoRhQ=="}, "X-Amz-Checksum-Sha1": {"x"}}, pre: true},
		"trailer ok":           {header: http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc64nvme"}}, trailers: map[string]string{"x-amz-checksum-crc64nvme": crc}},
		"trailer case":         {header: http.Header{"X-Amz-Trailer": {"X-Amz-Checksum-CRC64NVME"}}, trailers: map[string]string{"x-amz-checksum-crc64nvme": crc}},
		"trailer mismatch":     {header: http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc64nvme"}}, trailers: map[string]string{"x-amz-checksum-crc64nvme": "AAAAAAAAAAA="}, err: errChecksumMismatch},
		"trailer missing":      {header: http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc64nvme"}}, err: errInvalidChecksum},
		"trailer bad value":    {header: http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc64nvme"}}, trailers: map[string]string{"x-amz-checksum-crc64nvme": "AAAA"}, err: errInvalidChecksum},
		"trailer plus header":  {header: http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc32"}, "X-Amz-Checksum-Crc64nvme": {crc}}, pre: true},
		"two trailers":         {header: http.Header{"X-Amz-Trailer": {"x-amz-checksum-crc32, x-amz-checksum-sha1"}}, pre: true},
		"non-checksum trailer": {header: http.Header{"X-Amz-Trailer": {"x-amz-meta-foo"}}, none: true},
	}
	for name, tt := range tests {
		c, e, ok := requestChecksum(tt.header)
		switch {
		case tt.pre:
			if ok || e != ErrInvalidRequest {
				t.Errorf("%s: got ok=%v %v, want InvalidRequest", name, ok, e.Code)
			}
			continue
		case !ok:
			t.Errorf("%s: unexpected %v", name, e.Code)
			continue
		case tt.none:
			if c != nil {
				t.Errorf("%s: checksum %+v, want none", name, c)
			}
			continue
		case c == nil:
			t.Errorf("%s: no checksum", name)
			continue
		}
		if err := verifyBody(c, "hello world", tt.trailers); !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
			t.Errorf("%s: Verify = %v, want %v", name, err, tt.err)
		}
	}
}
