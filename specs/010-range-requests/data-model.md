# Data Model: Range Requests

No stored data changes.

| Entity | Fields | Rules |
|--------|--------|-------|
| Byte range | first, last (inclusive) | 0 ≤ first ≤ last < size after clamping; length = last − first + 1 |
| Range outcome | ignore / serve / unsatisfiable | per research R1 |
