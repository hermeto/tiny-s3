# Tasks: README and Architecture

**Input**: Design documents from `/specs/015-readme-architecture/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: The checks in `contracts/readme-outline.md` are run before the rewrite (they must fail on the current README) and after it (they must pass).

## Phase 1: Setup

- [X] T001 Run the structure checks of `quickstart.md` against the current `README.md` and record that they fail (wrong section order, no badge, no Mermaid, no failure table)

---

## Phase 2: Foundational

- [X] T002 Move the per-feature trade-off records 001–014 from the current `README.md` into one `<details>` block per feature, text unchanged in substance, for the new design section

---

## Phase 3: User Story 1 - Evaluate the project from the README alone (Priority: P1) 🎯 MVP

- [X] T003 [US1] Write the title, the CI badge for `main` and section 1 (what was built; what was left out and why) in `README.md`
- [X] T004 [US1] Write section 2 (run it with Docker: Compose, flags, exit codes, log lines including `recovery` and `storage_error`) in `README.md`

---

## Phase 4: User Story 2 - From clone to a working demo (Priority: P1)

- [X] T005 [US2] Write section 3 (use it with the aws CLI): one copy-paste walkthrough from `docker compose up` to `docker compose down -v` and the removal of the temporary directory it creates and works in, covering buckets, put with metadata, head, get with `cmp`, a range, listing with prefix and delimiter, `aws s3 ls`, delete, and pointing to `make e2e` and `scripts/demo.sh`
- [X] T006 [US2] Run the walkthrough exactly as written from a fresh clone of the branch and fix anything that differs (R4)

---

## Phase 5: User Story 3 - Understand the design and its guarantees (Priority: P1)

- [X] T007 [US3] Write section 4 (design): components Mermaid diagram, write-path Mermaid sequence diagram with the commit point, on-disk layout, index, concurrency, recovery, request handling, then the trade-off records from T002 plus a 015 record
- [X] T008 [US3] Write section 5 (failure modes): one table, failure / handling / proof, every proof an existing test name, a container-suite check name or a "Note:"
- [X] T009 [US3] Write section 6 (testing and CI): layers, make targets, cache variables, CI checks, publishing, protecting `main` with the ruleset command
- [X] T010 [US3] Write section 7 (what I would do next): the multipart upload design (R5) marked not implemented, Prometheus metrics as accepted technical debt, and the other next steps

---

## Phase 6: User Story 4 - See how AI was used (Priority: P2)

- [X] T011 [US4] Write section 8 (how AI was used) per research R6, every claim checkable in the repository

---

## Phase 7: Polish

- [X] T012 Run the structure checks of `quickstart.md`; all pass
- [X] T013 Run `make vet test race cover e2e`; all pass
- [X] T014 Render the README with GitHub's Markdown API (`gh api -X POST /markdown`, mode `gfm`) and check the badge image, two `mermaid` blocks and 15 `<details>` blocks in the HTML

## Dependencies & Execution Order

T001 → T002 → sections in README order (T003–T011) → T006 after T005 → Polish.

## Parallel Opportunities

None worth it: one file, written top to bottom.

## Implementation Strategy

Capture the failing checks, preserve the records, write the sections in order, validate the walkthrough from a clean clone.

## Analysis Remediation (2026-11-05)

`/speckit-analyze` findings, fixed with the recommended option:

| ID | Severity | Finding | Fix |
|----|----------|---------|-----|
| C1 | MEDIUM | The contract requires every failure row to cite a test, a container-suite check or a note, but the quickstart only checked `Test…` names. | Quickstart check 6 flags any row with none of the three. |
| U1 | MEDIUM | FR-003 ends on a clean machine, but T005 did not remove the walkthrough's temporary directory. | T005 removes it. |
| U2 | LOW | T014 ("renders on GitHub") had no way to be checked from the terminal. | T014 renders through GitHub's Markdown API and inspects the HTML. |

No constitution conflicts: the trade-off records stay in the README's design section. Coverage: FR-001–FR-011 and SC-001–SC-004 each map to at least one task (100%).
