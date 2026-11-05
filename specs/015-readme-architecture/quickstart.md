# Quickstart: README and Architecture

Contract: [contracts/readme-outline.md](contracts/readme-outline.md).

## Structure checks

```sh
grep '^## ' README.md                                   # 1
sed -n 3p README.md                                     # 2
grep -c '^```mermaid' README.md                         # 3
grep -c '^<details>' README.md                          # 4
for t in $(sed -n '/^## Failure modes/,/^## Testing/{/^## Testing/!p;}' README.md | grep -oE 'Test[A-Za-z0-9_]+' | sort -u); do
  grep -rqE "^func $t\(" internal e2e || echo "missing $t"
done                                                    # 5
sed -n '/^## Failure modes/,/^## Testing/p' README.md | grep '^| ' | tail -n +3 |
  grep -vE 'Test[A-Za-z0-9_]+|Note:|`(crash|object|range|list|delete|bucket)/' || echo "every row has a proof"   # 6
```

Expected: 1. the eight sections in order; 2. the CI badge; 3. `2`; 4. `15`; 5. no output; 6. `every row has a proof`.

## Walkthrough from a fresh clone

Clone the branch into a temporary directory and run the README's "Run it" and "Use it with the aws CLI" commands exactly as written; every output must match the README, and the last command leaves no container or volume.

## Gates

```sh
make vet test race cover e2e
```

Expected: all exit 0 (the README change must not affect anything else).
