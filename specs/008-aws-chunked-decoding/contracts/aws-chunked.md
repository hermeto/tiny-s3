# Contract: aws-chunked uploads and checksums

## Framing accepted on `PUT /<bucket>/<key>`

Signaled by `Content-Encoding` containing `aws-chunked` (case-insensitive, any position) or `X-Amz-Content-SHA256` starting with `STREAMING-`. The HTTP layer may also use `Transfer-Encoding: chunked`.

```text
stream  = *chunk last-chunk *trailer CRLF
chunk   = hex-size [ ";" extension ] CRLF data CRLF     ; data is hex-size bytes
last-chunk = "0" [ ";" extension ] CRLF
trailer = name ":" value CRLF                           ; at most 16
```

Every line (chunk header or trailer) is at most 4096 bytes including CRLF. Extensions (`chunk-signature=...`) and `x-amz-trailer-signature` are ignored.

| Header | Use |
|--------|-----|
| `X-Amz-Decoded-Content-Length` | Required; the object size |
| `X-Amz-Trailer` | Names the checksum trailer, e.g. `x-amz-checksum-crc64nvme` |

## Checksums (framed or plain uploads)

| Algorithm | Header / trailer name | Digest |
|-----------|----------------------|--------|
| CRC32 | `x-amz-checksum-crc32` | 4 bytes, big-endian |
| CRC32C | `x-amz-checksum-crc32c` | 4 bytes, big-endian |
| CRC64NVME | `x-amz-checksum-crc64nvme` | 8 bytes, big-endian |
| SHA1 | `x-amz-checksum-sha1` | 20 bytes |
| SHA256 | `x-amz-checksum-sha256` | 32 bytes |

Values are base64 (standard alphabet, padded). At most one checksum per request. Unknown `x-amz-checksum-*` names are ignored.

## Errors

| Condition | Status | Code | When |
|-----------|--------|------|------|
| Framed upload without decoded length | 411 | `MissingContentLength` | before body |
| Decoded length not a non-negative integer | 400 | `InvalidArgument` | before body |
| Decoded length over 5 GiB | 400 | `EntityTooLarge` | before body |
| Two checksums, or bad header value | 400 | `InvalidRequest` | before body |
| Malformed stream, long line, bad trailer value, announced trailer missing | 400 | `InvalidRequest` | during/after body |
| Stream ends early, or decoded bytes ≠ declared length | 400 | `IncompleteBody` | during/after body |
| Checksum mismatch | 400 | `BadDigest` | after body, before commit |

Nothing is stored on any error. A success answers 200 with `ETag` (MD5 of the decoded bytes), as in 007.

## Go APIs

```go
package awschunked
var ErrMalformed, ErrTruncated error
func NewReader(r io.Reader) *Reader
func (r *Reader) Read(p []byte) (int, error)
func (r *Reader) Trailer(name string) (string, bool) // after io.EOF; name is case-insensitive

package storage
type PutOptions struct { ...; Verify func() error } // called after the body, before the commit
```
