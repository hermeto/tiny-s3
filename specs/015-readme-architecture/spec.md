# Feature Specification: README and Architecture

**Feature Branch**: `015-readme-architecture`

**Created**: 2026-11-05

**Status**: Draft

**Input**: User description: "The README for tiny-s3, written for engineers evaluating the project. It must answer, in this order: what was built, what was deliberately left out and why; how to run it (Docker only); how to exercise it with the aws CLI, with copy-paste commands; the design (write path, commit point, on-disk layout, index, concurrency, recovery), with a Mermaid diagram of the write path and one of the components; the failure modes considered and how each is handled; how it is tested and validated in CI; what I would do next; and how AI was used (Spec Kit, Claude Code, and which decisions I made myself). Acceptance criteria: A reader with Docker and the aws CLI can go from clone to a working demo using only the README. A CI status badge for main is at the top. Every failure mode listed has a test or an explicit note. The multipart upload design is described in the next steps but not implemented. Accepted technical debt (Prometheus metrics) is listed."

## Clarifications

### Session 2026-11-05

Answered autonomously with the recommended option, as authorized by the feature owner for the remaining features.

- Q: Where do the per-feature trade-off records go in the reorganized README? → A: In the design section, as its last subsection, inside one collapsible `<details>` block per feature, so the README reads top-down while the records the constitution requires stay in the README's design section, unchanged in substance.
- Q: Which one-command validation does the walkthrough point to? → A: `make e2e` (Docker, make and Bash), with `bash scripts/demo.sh` as the narrated alternative.
- Q: In which voice is the README written? → A: Neutral, except "What I would do next" and "How AI was used", which are in the author's first person, as the request phrases them.
- Q: How is the failure-mode list laid out? → A: One table: the failure, how it is handled, and the proof (test names, or "Note:" with the reason there is no test).
- Q: Does the operational reference (flags, exit codes, log lines, make targets, CI checks, ruleset steps, cache variables) stay? → A: Yes, inside the section it belongs to (running, or testing and CI), so nothing documented today is lost.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Evaluate the project from the README alone (Priority: P1)

As an engineer evaluating tiny-s3, I read the README top to bottom and learn, in this order: what was built and what was left out on purpose and why; how to run it; how to use it with the S3 command-line client; how it is designed; which failures it handles and how; how it is tested; what comes next; and how AI was used to build it.

**Why this priority**: The README is the project's front door; an evaluator decides from it.

**Independent Test**: A reviewer checks that the README's top-level sections appear in exactly that order and that each answers its question.

**Acceptance Scenarios**:

1. **Given** the README, **When** a reader scans its top-level sections, **Then** they appear in the order: what was built and left out; running; using it with the aws CLI; design; failure modes; testing and CI; next steps; how AI was used.
2. **Given** the top of the README, **When** a reader opens it, **Then** a CI status badge for `main` is the first thing after the title.
3. **Given** the scope section, **When** a reader looks for what is missing, **Then** each deliberately excluded capability is listed with the reason.

---

### User Story 2 - From clone to a working demo (Priority: P1)

As an engineer with only Docker and the aws CLI installed, I follow the README's commands from a fresh clone and get a running server that stores, lists, reads and deletes objects.

**Why this priority**: If the instructions do not work as written, nothing else in the README is credible.

**Independent Test**: In a fresh clone on a machine with Docker and the aws CLI, run the README's commands exactly as written, in order, and confirm each produces the output the README shows.

**Acceptance Scenarios**:

1. **Given** a fresh clone, **When** the reader copies the run and usage commands in order, **Then** every command succeeds as described, ending with a clean shutdown.
2. **Given** the same machine, **When** the reader runs the one-command validation the README names, **Then** it passes.

---

### User Story 3 - Understand the design and its guarantees (Priority: P1)

As a reviewer, I understand how a write becomes durable (write path and commit point), how data is laid out on disk, how the index works, how concurrent requests are handled and how the server recovers from a crash, with one diagram of the write path and one of the components; I also see every failure mode considered and how it is handled, each backed by a test or an explicit note.

**Why this priority**: Durability is the project's first principle; the design section is where a reviewer checks it.

**Independent Test**: For each failure mode the README lists, find the named test in the repository or the explicit note explaining why there is none.

**Acceptance Scenarios**:

1. **Given** the design section, **When** a reviewer reads it, **Then** it covers the write path, commit point, on-disk layout, index, concurrency and recovery, with a Mermaid diagram of the write path and a Mermaid diagram of the components.
2. **Given** the failure-mode list, **When** each entry is checked, **Then** it names an existing test, or carries an explicit note why it is not tested.
3. **Given** the next steps, **When** a reader looks for multipart upload, **Then** its design is described there and stated as not implemented; accepted technical debt (Prometheus metrics) is listed.

---

### User Story 4 - See how AI was used (Priority: P2)

As an evaluator, I learn which tools were used (Spec Kit, Claude Code), how the work flowed, and which decisions the author made personally versus delegated.

**Why this priority**: Evaluators need to know what the author owns; it matters less than the technical content.

**Independent Test**: Every claim in the section can be checked against the repository (constitution, specs, clarification records, commit history).

**Acceptance Scenarios**:

1. **Given** the AI section, **When** a reader checks a claim (for example, who answered a feature's clarification questions), **Then** the repository's records support it.

### Edge Cases

- The trade-off records required by the constitution for every feature must stay in the README's design section, even though the README is reorganized.
- Instructions must not assume Go, a particular shell beyond POSIX `sh`, or local tools other than Docker, make and the aws CLI.
- Commands that create files must say where, and the walkthrough must end by removing what it created (container, volume, temporary files).
- Diagrams must render on GitHub (Mermaid fenced blocks) and stay readable as text.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The README MUST have top-level sections in this order: what was built and what was left out (with reasons); running it with Docker only; using it with the aws CLI (copy-paste commands); design; failure modes; testing and CI; what I would do next; how AI was used.
- **FR-002**: A CI status badge for the `main` branch MUST appear directly under the title.
- **FR-003**: The run and usage commands MUST take a reader with Docker, make and the aws CLI from a fresh clone to a working demo and back to a clean machine, exactly as written.
- **FR-004**: The design section MUST cover the write path, commit point, on-disk layout, index, concurrency and recovery, with a Mermaid diagram of the write path and one of the components.
- **FR-005**: Every listed failure mode MUST name the test(s) that cover it, or carry an explicit note why it is not tested.
- **FR-006**: The next steps MUST describe a multipart upload design and state it is not implemented, and MUST list the accepted technical debt (Prometheus metrics).
- **FR-007**: The AI section MUST name Spec Kit and Claude Code, describe the workflow, and state which decisions the author made, each claim checkable in the repository.
- **FR-008**: The per-feature trade-off records MUST be kept in the README's design section (constitution: Development Workflow), as its last subsection, one collapsible block per feature.
- **FR-009**: The walkthrough MUST name `make e2e` as the one-command validation and `scripts/demo.sh` as the narrated alternative.
- **FR-010**: The failure modes MUST be one table with the columns failure, handling and proof.
- **FR-011**: The operational reference (flags, exit codes, log lines, make targets, CI checks, ruleset steps, cache variables) MUST be kept in the section it belongs to.

### Key Entities

- **README section**: one of the eight questions, in order.
- **Failure mode entry**: a failure, how it is handled, and the covering test or note.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Following the README in a fresh clone, 100% of its commands run as written and produce the output it shows.
- **SC-002**: 100% of the failure modes listed cite an existing test or an explicit note.
- **SC-003**: The eight sections appear in the specified order, and the badge is the first element after the title.
- **SC-004**: A reader finds the multipart design, the accepted technical debt and the author's decisions without searching beyond their sections.

## Assumptions

- "Docker only" means the server runs only in Docker; the aws CLI is the reader's client tool, as the acceptance criteria state.
- The repository is private today, so the badge renders for readers with access; it is still the right badge for when it is public.
- Decisions attributed to the author are those the repository records as theirs: the constitution, each feature's description and acceptance criteria, the process rules, and the clarification answers not marked as answered autonomously.
