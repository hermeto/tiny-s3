# Contract: `make e2e`, `scripts/e2e/run.sh`, `scripts/demo.sh`

## Commands

| Command | Does | Exit |
|---------|------|------|
| `make e2e` | Go e2e tests, then `scripts/e2e/run.sh`, then `DEMO_PAUSE=0 scripts/demo.sh` | 0 only if all pass |
| `scripts/e2e/run.sh` | Container suite (checks below) | 0, or 1 at the first failed check |
| `scripts/demo.sh` | Narrated demo; `DEMO_PAUSE=0` disables pauses | 0, or 1 on failure |
| `make shellcheck` | shellcheck over `scripts/` (pinned image) | shellcheck's |

Host requirements: Docker and Bash. Output: one `PASS <check>` or `FAIL <check>: <reason>` line per check.

## Checks (in order)

| Check | Asserts |
|-------|---------|
| `bucket/create` | `create-bucket demo` exits 0 |
| `bucket/head` | `head-bucket demo` exits 0; `head-bucket missing` fails with 404 |
| `object/put-small`, `object/put-100MiB`, `object/put-unicode` | `put-object` exits 0 with ETag = quoted MD5 of the file; keys `small.txt`, `big.bin` (100 MiB), `dir/sub/ünïcødé ✓.txt` |
| `object/head` | `head-object` ContentLength and ETag match each file |
| `object/get` | `get-object` output equals each source file (`cmp`) |
| `range/first`, `range/from`, `range/suffix`, `range/invalid` | `bytes=0-99`, `bytes=100-`, `bytes=-50` equal the source slices; `bytes=<size>-` fails with `InvalidRange` |
| `list/prefix`, `list/delimiter` | over a small tree: prefix filters, delimiter yields the expected keys and `CommonPrefixes` |
| `list/paginate` | 2,500 keys: 5 pages of 500 by hand, 2,500 distinct keys in order; the CLI's paginator returns 2,500 |
| `delete/twice` | `delete-object` exits 0 twice; `head-object` then fails with 404 |
| `crash/kill` | SIGKILL lands while an upload is in `tmp/` |
| `crash/recovery` | after `docker start`: a `recovery` log line, empty `tmp/` |
| `crash/acknowledged` | every key the client saw acknowledged is listed |
| `crash/etags` | every listed crash key downloads to its source file and MD5 = ETag; `data/` holds one body per listed object |
