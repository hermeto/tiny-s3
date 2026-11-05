package s3api

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"hash"
	"hash/crc32"
	"hash/crc64"
	"io"
	"net/http"
	"strings"
)

// Errors from a checksum's Verify. They reach the client through the shared
// storage error mapping.
var (
	errChecksumMismatch = errors.New("s3api: checksum does not match the body")
	errInvalidChecksum  = errors.New("s3api: checksum trailer missing or invalid")
)

// crc64NVME is the CRC-64/NVME table (reflected polynomial; init and final
// XOR of all ones, which hash/crc64 applies).
var crc64NVME = crc64.MakeTable(0x9a6c9329ac4bc9b5)

// checksumAlg is a checksum algorithm clients may send.
type checksumAlg struct {
	name    string // header and trailer name, lowercase
	size    int    // digest bytes
	newHash func() hash.Hash
}

var checksumAlgs = []checksumAlg{
	{"x-amz-checksum-crc32", crc32.Size, func() hash.Hash { return crc32.NewIEEE() }},
	{"x-amz-checksum-crc32c", crc32.Size, func() hash.Hash { return crc32.New(crc32.MakeTable(crc32.Castagnoli)) }},
	{"x-amz-checksum-crc64nvme", crc64.Size, func() hash.Hash { return crc64.New(crc64NVME) }},
	{"x-amz-checksum-sha1", sha1.Size, sha1.New},
	{"x-amz-checksum-sha256", sha256.Size, sha256.New},
}

func findChecksumAlg(name string) (checksumAlg, bool) {
	for _, a := range checksumAlgs {
		if a.name == name {
			return a, true
		}
	}
	return checksumAlg{}, false
}

// checksum is the client's checksum of one upload: an algorithm and either
// the expected digest (from a header) or the trailer that will carry it.
type checksum struct {
	alg     checksumAlg
	want    []byte // from a header; nil when it comes from the trailer
	trailer bool
}

// requestChecksum returns the checksum a request declares, nil when it
// declares none, or InvalidRequest when it declares more than one or a
// header value that is not valid for its algorithm. Unknown algorithms are
// ignored.
func requestChecksum(h http.Header) (*checksum, APIError, bool) {
	var found []*checksum
	for _, alg := range checksumAlgs {
		v := h.Get(alg.name)
		if v == "" {
			continue
		}
		want, ok := decodeChecksum(alg, v)
		if !ok {
			return nil, ErrInvalidRequest, false
		}
		found = append(found, &checksum{alg: alg, want: want})
	}
	for _, field := range h.Values("X-Amz-Trailer") {
		for _, name := range strings.Split(field, ",") {
			if alg, ok := findChecksumAlg(strings.ToLower(strings.TrimSpace(name))); ok {
				found = append(found, &checksum{alg: alg, trailer: true})
			}
		}
	}
	switch len(found) {
	case 0:
		return nil, APIError{}, true
	case 1:
		return found[0], APIError{}, true
	}
	return nil, ErrInvalidRequest, false
}

// decodeChecksum decodes a base64 checksum value of alg.
func decodeChecksum(alg checksumAlg, v string) ([]byte, bool) {
	b, err := base64.StdEncoding.DecodeString(v)
	return b, err == nil && len(b) == alg.size
}

// wrap returns body teed into the checksum's hash, and the Verify callback
// for storage.PutOptions. trailer looks up a trailer once the body is read.
func (c *checksum) wrap(body io.Reader, trailer func(string) (string, bool)) (io.Reader, func() error) {
	h := c.alg.newHash()
	verify := func() error {
		want := c.want
		if c.trailer {
			v, ok := trailer(c.alg.name)
			if !ok {
				return errInvalidChecksum
			}
			if want, ok = decodeChecksum(c.alg, v); !ok {
				return errInvalidChecksum
			}
		}
		if !bytes.Equal(h.Sum(nil), want) {
			return errChecksumMismatch
		}
		return nil
	}
	return io.TeeReader(body, h), verify
}
