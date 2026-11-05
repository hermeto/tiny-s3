# Contract: README checks

| Check | How |
|-------|-----|
| Section order | `grep '^## ' README.md` prints the eight sections in R1 order |
| Badge | line 3 of README.md is the CI badge for `main` |
| Mermaid | exactly two ```` ```mermaid ```` blocks in the design section: components and write path |
| Failure proofs | every `Test…` name in the failure table has a `func Test…` in the repository; every check name exists in `scripts/e2e`; rows without a test start their proof with "Note:" |
| Multipart | the next-steps section describes it and says it is not implemented |
| Technical debt | the next-steps section lists Prometheus metrics as accepted debt |
| Walkthrough | the usage commands, run as written in a fresh clone, print what the README shows and leave nothing behind |
| Trade-off records | one `<details>` block per feature 001–015 in the design section |
