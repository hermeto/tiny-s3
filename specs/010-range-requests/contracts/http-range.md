# Contract: `Range` on GetObject and HeadObject

Evaluated after the bucket gate, the key lookup (404) and the 009 conditions (412/304).

| Request | Object size N | Answer |
|---------|---------------|--------|
| no `Range` | any | 200, whole object |
| `bytes=a-b`, a < N | N > 0 | 206, bytes a..min(b, N−1), `Content-Range: bytes a-min(b,N−1)/N` |
| `bytes=a-`, a < N | N > 0 | 206, bytes a..N−1 |
| `bytes=-k`, k > 0 | N > 0 | 206, last min(k, N) bytes |
| `bytes=a-…`, a ≥ N; `bytes=-0`; any range when N = 0 | — | 416 `InvalidRange`, `Content-Range: bytes */N` |
| two or more ranges; malformed (`bytes=abc`, `items=0-1`, `bytes=5-3`, overflow) | any | 200, whole object |
| `If-Range` not matching (other ETag, weak ETag, other date, garbage) | any | 200, whole object |

`bytes` is case-insensitive; spaces around the value and the `-` are allowed. 206 responses carry the 009 headers with `Content-Length` set to the range length. HEAD answers the same status and headers without a body. Error body for 416: `Code` `InvalidRange`, `Message` "The requested range is not satisfiable".
