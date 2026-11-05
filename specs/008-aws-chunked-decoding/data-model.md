# Data Model: aws-chunked Decoding

No stored data changes: the object record (005, 007) is unchanged, and checksums are verified, not stored (clarification 3).

## Request-scoped entities

| Entity | Fields | Rules |
|--------|--------|-------|
| Chunk | size (hex), extensions, data | size fits in 63 bits; header line ≤ 4096 bytes; data followed by CRLF |
| Trailer | name (lowercase), value | ≤ 16 per stream; line ≤ 4096 bytes; only the `x-amz-trailer` checksum is used |
| Checksum | algorithm, expected digest | one per request; source is a header or the announced trailer |
