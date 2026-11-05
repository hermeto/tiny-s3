# Research: aws-chunked Decoding

Every decision took the recommended option, as authorized by the feature owner.

## R1 What clients send (captures of aws CLI 2.31.11)

- **Plain HTTP endpoint**: `put-object` and `s3 cp` send a plain body with `Content-Length`, `x-amz-checksum-crc64nvme: <base64>` and `x-amz-sdk-checksum-algorithm: CRC64NVME`.
- **HTTPS endpoint**: `Transfer-Encoding: chunked` (no `Content-Length`), `Content-Encoding: aws-chunked`, `X-Amz-Content-SHA256: STREAMING-UNSIGNED-PAYLOAD-TRAILER`, `X-Amz-Decoded-Content-Length: 11`, `X-Amz-Trailer: x-amz-checksum-crc64nvme`, and a body of `b\r\nhello world\r\n0\r\nx-amz-checksum-crc64nvme:jSnVw/bqjr4=\r\n\r\n`. A 70,000-byte file went as one chunk.
- **With `AWS_REQUEST_CHECKSUM_CALCULATION=when_required`**: plain body, no checksum.
- **Signed framing** (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`, older SDKs): `<hex>;chunk-signature=<64 hex>\r\n<data>\r\n ... 0;chunk-signature=<64 hex>\r\n\r\n`; the `-TRAILER` variant adds trailers and `x-amz-trailer-signature:<hex>` after the final chunk.
- **Consequence**: net/http removes the HTTP chunking; the handler sees the aws-chunked stream with `ContentLength == -1`. The e2e tests reach the CLI's chunked path through a TLS-terminating reverse proxy in front of s3d (R7).

## R2 Decoder package

- **Decision**: New package `internal/awschunked` with `NewReader(r io.Reader) *Reader`. It reads through a 4096-byte `bufio.Reader`; chunk headers are `<hex size>[;<extension>]\r\n` (extensions such as `chunk-signature` ignored), chunk data is passed through without buffering, each chunk ends with `\r\n`; the zero-size chunk is followed by trailer lines `name:value\r\n` and an empty line, after which the stream must end. `Trailer(name)` returns a trailer (lowercase names) once `Read` returned `io.EOF`.
- **Errors**: `ErrMalformed` (bad hex, missing `\r\n`, line over 4096 bytes, trailer without `:`, more than 16 trailers, bytes after the end); `ErrTruncated` (the underlying reader ends early). Other errors from the underlying reader pass through.
- **Rationale**: A separate package keeps the framing parser small, fuzzable and 100% covered on its own; `bufio.Reader.ReadSlice` with a 4 KiB buffer bounds every line without extra code.
- **Alternatives considered**: Parsing inside `s3api` (mixes framing with HTTP concerns); `net/http/httputil.NewChunkedReader` (does not handle chunk extensions plus trailers the way aws-chunked uses them, and reports errors without the distinction we need).

## R3 Checksums

- **Decision**: `internal/s3api/checksum.go` knows CRC32 (IEEE), CRC32C (Castagnoli), CRC64NVME (`crc64.MakeTable(0x9a6c9329ac4bc9b5)`, the reflected NVMe polynomial), SHA1 and SHA256. The value is the base64 of the big-endian digest. A checksum comes from exactly one `x-amz-checksum-<alg>` header, or from the trailer named in `x-amz-trailer`; two sources (two headers, or a header plus a trailer) fail with `InvalidRequest`. A value that is not base64 or has the wrong length fails with `InvalidRequest` (header: before the body is read; trailer: after).
- **Verification**: the request body is wrapped in an `io.TeeReader` into the hash; `PutOptions.Verify` compares the digest with the expected value once the body is consumed.
- **Check**: CRC64NVME of `hello world` must be `jSnVw/bqjr4=` (from the capture).

## R4 Storage hook

- **Decision**: `storage.PutOptions` gains `Verify func() error`, called after the body is fully read and the MD5 checked, before the file sync. A non-nil error fails the Put (wrapped with `%w`), and the 005 cleanup removes the temp file.
- **Rationale**: The trailer is only known after the last body byte, and the commit must not happen before the check; one callback keeps the engine unaware of checksums.

## R5 Request flow in PutObject

Order (still before the first body read): bucket gate → framing: aws-chunked requires `X-Amz-Decoded-Content-Length` (absent → 411 `MissingContentLength`, not a non-negative integer → 400 `InvalidArgument`), else `Content-Length` (411) → `Content-MD5` → checksum header/trailer declaration and header value → metadata → `Store.Put` (key and size limits, including 5 GiB on the decoded length).

## R6 Error mapping

| Condition | S3 answer |
|-----------|-----------|
| `awschunked.ErrMalformed` | 400 `InvalidRequest` |
| `awschunked.ErrTruncated` | 400 `IncompleteBody` |
| checksum mismatch | 400 `BadDigest` |
| invalid checksum value, two checksums, announced trailer missing | 400 `InvalidRequest` |
| decoded bytes ≠ declared length | 400 `IncompleteBody` (engine, 005) |

`InvalidRequest` message: "Invalid Request". `BadDigest` keeps its 007 message.

## R7 Real-client e2e through TLS

- **Decision**: An e2e helper starts `httptest.NewTLSServer` with `httputil.NewSingleHostReverseProxy` to s3d, writes the server certificate to a file and runs the aws CLI with `AWS_CA_BUNDLE` pointing at it and `--endpoint-url https://127.0.0.1:<port>`. The CLI then uses its default chunked framing; the proxy forwards the stream unchanged. ETag equality with the file's MD5 proves the stored bytes (reading back is feature 009).
- **Alternatives considered**: `--no-verify-ssl` (prints warnings, hides certificate mistakes); TLS in s3d (scope creep); the AWS SDK for Go (allowed in e2e, but adds a module and go.sum for what the CLI already proves).

## R8 Bounded memory

- **Decision**: An `awschunked` test decodes a lazily generated 64 MiB single-chunk stream through `io.CopyBuffer` and requires `TotalAlloc` growth below 4 MiB; the 4096-byte line limit is tested with a 4097-byte chunk header.
