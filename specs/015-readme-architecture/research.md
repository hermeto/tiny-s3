# Research: README and Architecture

Every decision took the recommended option, as authorized by the feature owner.

## R1 Outline

Eight `##` sections in the required order, after the title and the badge:

1. What tiny-s3 is: what was built; what was left out, and why
2. Run it (Docker only): Compose, flags, exit codes, logs
3. Use it with the aws CLI: a copy-paste walkthrough from an empty server to a clean machine
4. Design: components (Mermaid), write path and commit point (Mermaid), on-disk layout, index, concurrency, recovery, request handling; then the per-feature trade-off records, one `<details>` block per feature (constitution)
5. Failure modes: one table, failure / handling / proof
6. Testing and CI: test layers, make targets, CI checks, publishing, protecting `main`
7. What I would do next: multipart upload design (not implemented), accepted technical debt (Prometheus metrics), other next steps
8. How AI was used: tools, workflow, the author's decisions, what was delegated

## R2 Keeping the trade-off records

The constitution requires every feature's trade-offs in the README's design section. The 14 existing records move, unchanged in substance, into `<details>` blocks at the end of the design section, and 015 adds its own. GitHub renders `<details>` in Markdown, so the README reads top-down without losing them.

## R3 Proof for failure modes

Each table row cites test function names that exist in the repository (`internal/**/_test.go`, `e2e/`) or the container suite's check names (`scripts/e2e/checks.sh`, `run.sh`), or carries "Note:" with why it is not tested (power loss, disk full beyond injected write errors, two processes on one data directory, bit rot at rest). A one-off check extracts every `Test…` name from the table and greps for its definition.

## R4 Walkthrough validation

The usage commands are run exactly as written from a fresh clone (`git clone` of the branch into a temporary directory) with Docker, make and the aws CLI; every printed result must match the README. The walkthrough writes only under one temporary directory it creates and ends with `docker compose down -v`.

## R5 Multipart upload design (next steps, not implemented)

Parts are immutable files under `uploads/<uploadId>/` written with the same temp-sync-rename-dirsync order; CompleteMultipartUpload validates the part list (order, ETags, 5 MiB minimum except the last), then commits one record that lists the part files instead of copying them (the record stays the commit point); GET and ranges stream across the listed parts; the ETag is the MD5 of the concatenated part MD5s plus `-N`; Abort removes the directory; recovery keeps in-progress uploads and removes parts no record or live upload names. Described in the README only, as the constitution says.

## R6 Attribution in "How AI was used"

Claims are limited to what the repository records: the constitution (ratified by the author), each feature's description and acceptance criteria (each spec's Input), the process rules given to the agent (Spec Kit flow, TDD, 100% coverage, gates before a PR, Conventional Commits, signed commits), the clarification answers of feature 003 (no autonomy note), and the delegation recorded in features 005–015 (clarifications, analysis fixes and design choices taken by Claude Code with the recommended option, each recorded in the feature's artifacts). Because the section speaks in the author's voice, the PR asks the author to review it.
