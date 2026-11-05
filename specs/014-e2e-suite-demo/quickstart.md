# Quickstart: End-to-End Suite and Demo Script

Contract: [contracts/suite.md](contracts/suite.md).

## Quality gates

```sh
make vet test race cover e2e
make shellcheck
```

Expected: all exit 0; `make cover` at 100% for every `internal/` package; `make e2e` prints `PASS` for every check of the container suite and ends with the demo's last step; `make shellcheck` prints nothing.

## Run the suite and the demo directly

```sh
bash scripts/e2e/run.sh            # 1
DEMO_PAUSE=0 bash scripts/demo.sh  # 2
docker ps -a --filter name=tiny-s3-e2e- --format '{{.Names}}'; docker volume ls --filter name=tiny-s3-e2e- -q; docker network ls --filter name=tiny-s3-e2e- -q   # 3
```

Expected:

1. One `PASS` line per check in `contracts/suite.md`, ending with `e2e suite: PASSED`; exit 0.
2. Ten narrated steps, ending with `demo complete`; exit 0.
3. No output: nothing left behind.

## Compose check

The Compose stack is unaffected by the suite:

```sh
docker compose up -d --build
bash scripts/e2e/run.sh >/dev/null && echo suite-ok
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:9000/   # 200: the Compose server still answers
docker compose down -v
```
