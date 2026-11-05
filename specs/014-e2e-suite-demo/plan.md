# Implementation Plan: End-to-End Suite and Demo Script

**Branch**: `014-e2e-suite-demo` | **Date**: 2026-11-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/014-e2e-suite-demo/spec.md`

## Summary

Extend `make e2e` with a container suite (`scripts/e2e/run.sh`) that builds the server image, runs it on a fresh volume in a private network, drives every operation with the real aws CLI from a client container (checks in `scripts/e2e/checks.sh`), kills the container mid-upload and verifies recovery; then runs `scripts/demo.sh` without pauses. CI gets a `make e2e` job that `publish` waits for.

## Technical Context

**Language/Version**: Bash 3.2+ on the host (macOS default) and Bash 5 in the client image; Go 1.27.3 for the existing tests

**Primary Dependencies**: Docker; the pinned aws-cli image (now with pinned `diffutils`); the pinned shellcheck image for `make shellcheck`

**Storage**: Fresh named volume per run

**Testing**: The suite is the test; `make shellcheck` for the scripts

**Target Platform**: Developer machines (macOS, Linux) and GitHub-hosted ubuntu-24.04 runners

**Project Type**: Test tooling and CI

**Performance Goals**: `make e2e` within 15 minutes in CI; demo under 3 minutes without pauses

**Constraints**: No host ports, no Compose names; cleanup on every exit path

**Scale/Scope**: 100 MiB object, 2,500 keys, 20 × 32 MiB crash uploads

## Constitution Check

- **I–III**: PASS (no production code change).
- **IV. Test-First**: PASS. The suite's checks are written before the scripts that run them pass; `internal/` coverage untouched.
- **V. Real-Client Compatibility**: PASS. Every operation exercised by `aws s3api` against the shipped image.
- **Development Workflow**: PASS. A new CI check named after its command; required checks unchanged (the ruleset change is the admin's, documented).
- **Bash quality** (owner's standard): `set -euo pipefail`, shellcheck-clean.

Post-design re-check: PASS.

## Project Structure

```text
specs/014-e2e-suite-demo/  spec plan research data-model quickstart contracts/suite.md tasks

scripts/e2e/lib.sh      # names, start/stop server, client and inspector, cleanup trap, check helpers
scripts/e2e/run.sh      # the suite (host side): build, start, checks, crash, verdict
scripts/e2e/checks.sh   # the checks (inside the client container)
scripts/demo.sh         # narrated demo
e2e/Dockerfile          # + diffutils for cmp
Makefile                # e2e runs the three steps; shellcheck target
.github/workflows/ci.yml  # make e2e job; publish needs it
README.md               # how to run, CI table, ruleset command, 014 design section
```

## Design Decisions

See [research.md](research.md): R1 shape, R2 isolation, R3 CLI, R4 crash, R5 output, R6 CI, R7 demo, R8 shell quality.

## Complexity Tracking

No constitution violations.
