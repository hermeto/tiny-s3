# Contract: Makefile cache variables (change to 001's developer targets)

The targets, their commands and exit statuses are unchanged from
[001's make contract](../../001-server-skeleton/contracts/make.md). This feature adds two variables that choose
where the Go caches are mounted from.

| Variable | Default | Mounted at | Used by |
|----------|---------|------------|---------|
| `GOMOD_CACHE` | `tiny-s3-gomod` (named volume) | `/go/pkg/mod` | `test`, `race`, `vet`, `cover`, `e2e` |
| `GOBUILD_CACHE` | `tiny-s3-gobuild` (named volume) | `/root/.cache/go-build` | same |

- The value is passed verbatim as the source of `docker run -v <value>:<target>`. A bare name is a named volume;
  an absolute path is a bind mount (CI uses `$RUNNER_TEMP/gomod` and `$RUNNER_TEMP/gobuild`).
- Set them on the command line (`make test GOMOD_CACHE=/abs/path`) or in the environment.
- The names deliberately differ from Go's `GOMODCACHE` and `GOCACHE`, so a host Go installation's environment
  never leaks into the container.
- `make clean-cache` still removes only the default named volumes.

Running `make <target>` with neither variable set behaves exactly as before this feature.
