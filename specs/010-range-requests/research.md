# Research: Range Requests

Every decision took the recommended option, as authorized by the feature owner.

## R1 Parsing

- **Decision**: `parseRange(header string, size int64) (rangeResult)` in `internal/s3api/rangereq.go`, with three outcomes: `rangeIgnore` (no header, malformed, multiple ranges → serve the whole object, 200), `rangeServe{first, last}` (206) and `rangeUnsatisfiable` (416). Grammar: optional spaces, case-insensitive `bytes`, `=`, then exactly one spec: `first-last`, `first-` or `-suffix`, digits only, each fitting in 63 bits, spaces allowed around `-`. A comma anywhere means multiple ranges (ignored). `first > last` is malformed (ignored). On a size-0 object every well-formed range is unsatisfiable. `first >= size` is unsatisfiable; `last` is clamped to `size-1`; `-0` is unsatisfiable; a suffix longer than the object serves `0..size-1`.
- **Rationale**: Matches S3 and RFC 9110 for single ranges; S3 ignores multiple ranges instead of answering multipart/byteranges.
- **Alternatives considered**: `net/http`'s internal range parser (unexported; `ServeContent` answers multi-range requests with multipart bodies, unlike S3).

## R2 If-Range

- **Decision**: `ifRangeMatches(v string, info) bool`: a value starting with `W/` never matches; a value that parses as an HTTP date matches when it equals the last-modified time truncated to seconds; otherwise it matches when, unquoted, it equals the hex MD5. When `If-Range` is present and does not match, the `Range` is ignored.

## R3 Order in GET and HEAD

`Get`/`Head` (404) → 009 conditions (412/304) → `If-Range` → `parseRange`. 416 answers `InvalidRange` with `Content-Range: bytes */size` (HEAD: no body). 206 writes the 009 headers, then overrides `Content-Length` with the range length and adds `Content-Range: bytes first-last/size`; GET streams `io.NewSectionReader(rd, first, length)` through the same 32 KiB buffer and abort-on-failure path as 009.

## R4 Errors

| Condition | Answer |
|-----------|--------|
| Unsatisfiable | 416 `InvalidRange`, "The requested range is not satisfiable", `Content-Range: bytes */size` |

## R5 Proofs

- Table tests for `parseRange` and `ifRangeMatches`; handler tests for GET and HEAD; a read fault inside a range aborts the connection.
- e2e: `aws s3api get-object --range` with the three forms, a clamped end, `bytes=N-` (`InvalidRange`), multiple and malformed ranges (whole object); `If-Range` through a Go client (the CLI has no `--if-range`); the 2 GiB test requests its second half (`bytes=1073741824-`) and keeps peak RSS below 64 MiB.
