# Contract: `s3d` command line and process behaviour

```text
s3d [-listen ADDR] [-data-dir PATH]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-listen` | `:9000` | TCP address to listen on (`host:port`; empty host = all interfaces; port `0` = pick a free port). |
| `-data-dir` | `/data` | Directory for object data. Created if missing; must be writable. |
| `-h`, `-help` | | Print usage to stderr and exit 0. |

## Exit codes

| Code | When |
|------|------|
| 0 | Shut down by SIGTERM/SIGINT and every in-flight request completed; or `-h`. |
| 1 | Data directory unusable, listen failed, the HTTP server failed, or shutdown was forced at the deadline. |
| 2 | Usage error: unknown flag, malformed or empty value, positional argument. Usage is printed to stderr. |

## Signals

| Signal | Effect |
|--------|--------|
| SIGTERM, SIGINT (first) | Stop accepting connections, close idle connections, wait up to 9 s for active requests, then close the rest. Process exits within 10 s of the signal. |
| SIGTERM, SIGINT (later) | Ignored; the drain continues under the original deadline. |

## Log output (stdout, JSON lines)

| `msg` | Attributes | When |
|-------|------------|------|
| `listening` | `listen` (actual address), `data_dir` | Listener open and data directory checked. |
| `shutdown_started` | `cause` (`context.Cause` of the root context, e.g. `interrupt signal received`) | First signal received. |
| `shutdown_complete` | `elapsed` | All requests drained. |
| `shutdown_forced` | `elapsed` | Deadline reached; connections closed. |

Startup errors are written to stderr as one plain-text line: `s3d: <reason>`.
