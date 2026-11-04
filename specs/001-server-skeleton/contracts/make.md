# Contract: developer targets

All targets run in the pinned golang container (see [research.md](../research.md) R1–R2). Host requirements:
Docker with Compose v2, and `make` (GNU make 3.81 or later).

| Target | Runs in the container | Exit status |
|--------|-----------------------|-------------|
| `make test` | `go test ./...` | 0 if all tests pass. |
| `make race` | `go test -race ./...` | 0 if all tests pass and no race is reported. |
| `make vet` | `go vet -tags e2e ./...` | 0 if vet reports nothing (the tag includes the e2e package). |
| `make cover` | `scripts/cover-gate.sh` | 0 only if every package under `internal/` has tests and `covered == total` statements. |
| `make e2e` | `go test -tags e2e -count=1 ./e2e/...` | 0 if the real `s3d` binary passes the signal and exit-code tests (research.md R11). |

The gate compares integer statement counts, never the rounded percentage: 2499/2500 shows as `100.0%` but
fails. A package with no test files (`go list` reports no `TestGoFiles` or `XTestGoFiles`) is marked
`FAIL (no tests)` and shows its real statement total from the profile.

`make cover` output, one line per package, then a verdict:

```text
PACKAGE                                        COVERED/TOTAL  PERCENT
github.com/hermeto/tiny-s3/internal/config            41/41   100.0%
github.com/hermeto/tiny-s3/internal/server            57/58    98.3%  FAIL
github.com/hermeto/tiny-s3/internal/newpkg             0/12     0.0%  FAIL (no tests)
coverage gate: FAILED (2 packages below 100%)
```

Caches live in the named volumes `tiny-s3-gomod` and `tiny-s3-gobuild`. `make clean-cache` removes them.
