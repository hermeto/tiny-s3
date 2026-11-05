# Tasks: README and Architecture

**Input**: Design documents from `/specs/015-readme-architecture/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: The checks in `contracts/readme-outline.md` are run before the rewrite (they must fail on the current README) and after it (they must pass).

## Phase 1: Setup

- [ ] T001 Run the structure checks of `quickstart.md` against the current `README.md` and record that they fail (wrong section order, no badge, no Mermaid, no failure table)

---

## Phase 2: Foundational

- [ ] T002 Move the per-feature trade-off records 001–014 from the current `README.md` into one `<details>` block per feature, text unchanged in substance, for the new design section

---

## Phase 3: User Story 1 - Evaluate the project from the README alone (Priority: P1) 🎯 MVP

- [ ] T003 [US1] Write the title, the CI badge for `main` and section 1 (what was built; what was left out and why) in `README.md`
- [ ] T004 [US1] Write section 2 (run it with Docker: Compose, flags, exit codes, log lines including `recovery` and `storage_error`) in `README.md`

---

## Phase 4: User Story 2 - From clone to a working demo (Priority: P1)

- [ ] T005 [US2] Write section 3 (use it with the aws CLI): one copy-paste walkthrough from `docker compose up` to `docker compose down -v`, working in a temporary directory it creates, covering buckets, put with metadata, head, get with `cmp`, a range, listing with prefix and delimiter, `aws s3 ls`, delete, and pointing to `make e2e` and `scripts/demo.sh`
- [ ] T006 [US2] Run the walkthrough exactly as written from a fresh clone of the branch and fix anything that differs (R4)

---

## Phase 5: User Story 3 - Understand the design and its guarantees (Priority: P1)

- [ ] T007 [US3] Write section 4 (design): components Mermaid diagram, write-path Mermaid sequence diagram with the commit point, on-disk layout, index, concurrency, recovery, request handling, then the trade-off records from T002 plus a 015 record
- [ ] T008 [US3] Write section 5 (failure modes): one table, failure / handling / proof, every proof an existing test name, a container-suite check name or a "Note:"
- [ ] T009 [US3] Write section 6 (testing and CI): layers, make targets, cache variables, CI checks, publishing, protecting `main` with the ruleset command
- [ ] T010 [US3] Write section 7 (what I would do next): the multipart upload design (R5) marked not implemented, Prometheus metrics as accepted technical debt, and the other next steps

---

## Phase 6: User Story 4 - See how AI was used (Priority: P2)

- [ ] T011 [US4] Write section 8 (how AI was used) per research R6, every claim checkable in the repository

---

## Phase 7: Polish

- [ ] T012 Run the structure checks of `quickstart.md`; all pass
- [ ] T013 Run `make vet test race cover e2e`; all pass
- [ ] T014 Check the README renders on GitHub after the push (badge, both Mermaid diagrams, `<details>` blocks)

## Dependencies & Execution Order

T001 → T002 → sections in README order (T003–T011) → T006 after T005 → Polish.

## Parallel Opportunities

None worth it: one file, written top to bottom.

## Implementation Strategy

Capture the failing checks, preserve the records, write the sections in order, validate the walkthrough from a clean clone.
