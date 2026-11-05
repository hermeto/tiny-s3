# Research: End-to-End Suite and Demo Script

Every decision took the recommended option, as authorized by the feature owner.

## R1 Shape of `make e2e`

- **Decision**: three steps, each failing the target: (1) the existing Go tests of the server binary in the e2e image (001–013, about 90 s); (2) `scripts/e2e/run.sh`, the container suite against the image built from the repository's `Dockerfile`; (3) `DEMO_PAUSE=0 scripts/demo.sh`.
- **Rationale**: The Go tests cover process behavior (signals, raw protocol, memory) the image suite cannot; the image suite proves the shipped artifact with the real CLI; running the demo keeps it from rotting.

## R2 Isolation and cleanup

- **Decision**: every run uses names with a unique suffix (`tiny-s3-e2e-<pid>-…`): one Docker network, one data volume, the server container (network alias `s3d`, no published port), a client container (the e2e image: aws CLI + `cmp`) that runs the checks through `docker exec`, and an inspector container that mounts the data volume read-only to look at `tmp/` and `data/`. A `trap` on `EXIT INT TERM` removes all of them. Nothing touches the Compose project, port 9000 or `tiny-s3_data`.
- **Rationale**: Developers may have the Compose stack running; CI runners must not leak resources between steps.

## R3 Running the CLI

- **Decision**: one long-lived client container per run; checks are bash functions in `scripts/e2e/checks.sh`, mounted read-only and run with `docker exec`. The CLI runs with its default settings (no `AWS_MAX_ATTEMPTS`, no checksum options), with throwaway credentials and region `us-east-1`. 2,500 small objects are uploaded with `aws s3 cp --recursive` (parallel, fast); every check uses `aws s3api`.
- **`cmp`**: the e2e image installs `diffutils-3.8-1.amzn2023.0.2` with `dnf`; Amazon Linux 2023 pins its repositories to the image's release, and the explicit version makes the build fail rather than drift.

## R4 Crash test

- **Decision**: the host starts 20 parallel `aws s3api put-object` uploads of distinct 32 MiB files in the client (each records an ack file when the CLI exits 0), polls the inspector every 50 ms until `tmp/` holds a file, and sends `docker kill -s KILL` to the server. After the uploads exit (failed or acknowledged), it runs `docker start` on the same container and volume, waits for the second `listening` line, and requires: a `recovery` line from the restart; an empty `tmp/`; every acknowledged key listed; every listed key downloading to bytes equal to its source file with MD5 equal to its ETag; `data/` holding as many files as listed objects.
- **Rationale**: The temp file proves the kill landed mid-upload; acks give the client's view of what must survive.

## R5 Output and failure

- **Decision**: each check prints `PASS <name>` or `FAIL <name>: <reason>`; the first failure stops the suite with exit 1 and its name. On failure the host prints the server's log tail.

## R6 CI

- **Decision**: a job `make e2e` (ubuntu-24.04, 30-minute timeout) on the workflow's existing triggers, restoring and (on main) saving its own Go build cache like the gates; `publish` gains `needs: e2e`. The ruleset is not changed here; the README's ruleset command adds `make e2e` for the admin to apply.

## R7 Demo

- **Decision**: `scripts/demo.sh` shares `scripts/e2e/lib.sh` with the suite; ten narrated steps (start, bucket, upload, download and compare, range, folders, crash and recovery, delete, cleanup), each echoing the command it runs, pausing with `read` unless `DEMO_PAUSE=0`.

## R8 Shell quality

- **Decision**: `set -euo pipefail` everywhere; a `make shellcheck` target runs `koalaman/shellcheck:v0.10.0@sha256:2097951f02e735b613f4a34de20c40f937a6c8f18ecb170612c88c34517221fb` over `scripts/`.
