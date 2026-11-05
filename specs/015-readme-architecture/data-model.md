# Data Model: README and Architecture

No code or stored data changes. The README's structure:

| # | Section | Must contain |
|---|---------|--------------|
| — | Title + badge | CI badge for `main` right under the title |
| 1 | What tiny-s3 is | operations built; exclusions with reasons |
| 2 | Run it | Compose commands; flags; exit codes; log lines |
| 3 | Use it with the aws CLI | copy-paste walkthrough; `make e2e` and the demo |
| 4 | Design | components and write-path Mermaid diagrams; commit point; layout; index; concurrency; recovery; per-feature trade-off records |
| 5 | Failure modes | table: failure, handling, proof |
| 6 | Testing and CI | layers; make targets; CI checks; publishing; ruleset |
| 7 | What I would do next | multipart design (not implemented); Prometheus metrics as accepted debt; others |
| 8 | How AI was used | Spec Kit, Claude Code; workflow; author's decisions; delegation |
