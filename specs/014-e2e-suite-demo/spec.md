# Feature Specification: End-to-End Suite and Demo Script

**Feature Branch**: `014-e2e-suite-demo`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "An end-to-end test suite and a demo script that prove tiny-s3 works with real S3 tooling. Anyone must be able to run one command to see every implemented operation exercised by the real aws CLI v2 with default settings, and there is a scripted demo for live walkthroughs. Acceptance criteria: make e2e builds the image, starts the server with a fresh named volume, and runs scripted checks with aws s3api: create-bucket and head-bucket; put-object (small, 100 MiB, keys with slashes and unicode); head-object, and get-object compared with cmp; get-object with the three range forms and an invalid range; list-objects-v2 with prefix, delimiter and pagination over 2,500 keys; delete-object twice; a crash test that sends SIGKILL to the container during parallel uploads, restarts it and verifies that every listed object matches its ETag. The suite exits non-zero on any failure. The CI workflow from spec 002 runs make e2e as its own check on every pull request and on main. scripts/demo.sh runs a shorter, narrated version step by step, pausing between steps."

## Clarifications

### Session 2026-11-05

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: Does `make e2e` keep the existing end-to-end tests of the server binary, and in what order? → A: Yes. It runs them first (fast feedback, about 90 s), then the container suite, then the demo without pauses; all three must pass.
- Q: How does the crash test make sure the kill lands during uploads? → A: It starts 20 parallel `aws s3api put-object` uploads of distinct 32 MiB files and watches the volume's temporary area through a helper container; it sends SIGKILL as soon as an upload is in flight there. After the restart it requires the recovery line in the log, an empty temporary area, every upload the client saw acknowledged listed, and every listed object downloading to bytes that equal its source file and its ETag.
- Q: How is pagination over 2,500 keys verified? → A: By paging by hand with `--no-paginate --max-keys 500` and the continuation token, expecting 5 pages and 2,500 distinct keys in order, plus the command-line client's own paginator returning 2,500 keys.
- Q: How is the CI check set up? → A: A job named `make e2e` on the same triggers as the other checks (every push, every pull request to main), with the same Go cache handling and a 30-minute timeout; the publish job waits for it. The ruleset is not changed by this feature; the README's ruleset command lists the new check.
- Q: How are the demo's pauses turned off, and what keeps the demo from rotting? → A: `DEMO_PAUSE=0` turns them off; `make e2e` runs the demo that way after the suite.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One command proves every operation against the shipped image (Priority: P1)

As an engineer evaluating tiny-s3, I run one command and watch every implemented S3 operation exercised by the real S3 command-line client against the same container image that would be deployed, ending with a clear pass or fail.

**Why this priority**: It is the proof that the project works with real tooling, from the artifact users actually run.

**Independent Test**: Run the command on a machine with only Docker and make; it must pass on a correct build and fail when any operation misbehaves.

**Acceptance Scenarios**:

1. **Given** a machine with Docker and make, **When** the engineer runs `make e2e`, **Then** it builds the server image, starts it on a fresh named volume, and runs scripted checks with the S3 command-line client in its default configuration.
2. **Given** the running image, **When** the checks run, **Then** they cover: creating and checking a bucket; uploading a small object, a 100 MiB object and keys with slashes and non-ASCII characters; fetching metadata and downloading each object, compared byte for byte with the source; downloading the three range forms and an unsatisfiable range; listing with prefix, delimiter and pagination over 2,500 keys; deleting an object twice.
3. **Given** any check fails, **When** the suite ends, **Then** it exits non-zero and names the failed check; with all checks passing it exits 0.
4. **Given** the suite ran, **When** it ends (pass or fail), **Then** the containers, network and volume it created are removed.

---

### User Story 2 - Crash test against the container (Priority: P1)

As an engineer, I see the server container killed abruptly during parallel uploads and restarted on the same volume, and every object it then lists downloads with bytes matching its ETag.

**Why this priority**: Durability is the project's first principle; this shows it on the shipped image, not only on a test binary.

**Independent Test**: Run the crash part of the suite on its own and inspect its verdict.

**Acceptance Scenarios**:

1. **Given** parallel uploads in progress, **When** the container receives SIGKILL and is started again on the same volume, **Then** every listed object downloads completely and its MD5 equals its ETag.
2. **Given** the same restart, **When** the server starts, **Then** its startup log reports the recovery it performed.

---

### User Story 3 - The checks run in CI on every change (Priority: P1)

As a maintainer, the same command runs as its own CI check on every pull request and every push to main, so a change that breaks real-client compatibility is caught before merge.

**Why this priority**: Without CI, the suite rots and stops being a proof.

**Independent Test**: Open a pull request and see a separate check named after the command; break an operation on purpose and see the check fail.

**Acceptance Scenarios**:

1. **Given** a pull request or a push to main, **When** CI runs, **Then** a check named `make e2e` runs the same command as developers run locally.
2. **Given** a push to main, **When** the image would be published, **Then** publishing waits for the end-to-end check as well.

---

### User Story 4 - A narrated demo for live walkthroughs (Priority: P2)

As a presenter, I run a demo script that narrates each step, runs a shorter version of the checks, and pauses between steps so I can talk; it can also run without pauses.

**Why this priority**: Useful for presentations, not needed for correctness.

**Independent Test**: Run the demo with pauses and without; it must finish successfully and clean up after itself.

**Acceptance Scenarios**:

1. **Given** the demo is started, **When** each step begins, **Then** the script prints what it will show, runs it, shows the result, and waits for Enter before the next step.
2. **Given** the demo is run without pauses (for rehearsal or automation), **When** it ends, **Then** it exits 0 having run every step, and removes what it created.

### Edge Cases

- The suite must not depend on, or disturb, a tiny-s3 stack the developer is already running (Compose project, port 9000, `tiny-s3_data` volume): it uses its own names and no published ports.
- A suite interrupted with Ctrl-C still removes its containers, network and volume.
- The crash test cannot know which uploads were acknowledged before the kill from the server's side; it verifies that whatever is listed is complete and that every upload acknowledged to the client is listed.
- The host may have no S3 command-line client; the suite and the demo run it in a container.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: `make e2e` MUST build the server image from the repository, start it with a fresh named volume, run the scripted checks with the real S3 command-line client v2 in its default configuration, and exit non-zero on any failed check.
- **FR-002**: The scripted checks MUST cover every acceptance item: create-bucket, head-bucket, put-object (small, 100 MiB, keys with slashes and non-ASCII), head-object, get-object compared byte for byte, the three range forms and an unsatisfiable range, list-objects-v2 with prefix, delimiter and pagination over 2,500 keys, delete-object twice, and the crash test.
- **FR-003**: The crash test MUST send SIGKILL to the server container during parallel uploads, start it again on the same volume, and verify every listed object's bytes against its ETag and that every upload the client saw acknowledged is listed.
- **FR-004**: The suite MUST clean up every container, network and volume it created, on success, failure or interruption, and MUST NOT use the Compose project's names or publish ports on the host.
- **FR-005**: The existing end-to-end tests of the server binary (features 001–013) MUST keep running as part of `make e2e`.
- **FR-006**: CI MUST run `make e2e` as its own check, named `make e2e`, on every pull request to main and every push, and the image publish job MUST wait for it.
- **FR-007**: `scripts/demo.sh` MUST narrate and run a shorter version of the checks step by step, pausing for Enter between steps unless told not to pause, and clean up after itself.
- **FR-008**: The scripts MUST need only Docker and a POSIX shell environment with Bash on the host, and MUST pass shellcheck.
- **FR-009**: `make e2e` MUST run, in order, the binary end-to-end tests, the container suite and the demo with pauses off.
- **FR-010**: The crash test MUST send the kill only once an upload is in flight in the volume's temporary area, and after the restart MUST require the recovery log line and an empty temporary area.

### Key Entities

- **Suite run**: one isolated set of server container, network and volume, plus the checks and their verdict.
- **Check**: a named step with a pass/fail result.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a correct build, `make e2e` passes in 100% of runs on a developer machine and in CI, and finishes within 15 minutes in CI.
- **SC-002**: Breaking any single covered operation makes `make e2e` exit non-zero with the failing check named.
- **SC-003**: After any run (pass, fail or interrupted), 0 containers, networks or volumes created by the suite remain.
- **SC-004**: The demo runs end to end without pauses in under 3 minutes and exits 0.

## Assumptions

- Making the new CI check required for merging is a repository ruleset change done by an admin; the README gives the command, and this feature does not change the ruleset itself.
- The S3 command-line client used is the digest-pinned image from feature 006, extended with the `cmp` tool from its own distribution's pinned package version.
- Uploading 2,500 keys uses the command-line client's high-level copy command for speed; listing, pagination and every other check use `aws s3api`.
