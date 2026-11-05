# Data Model: DeleteObject

No format change. A delete removes `meta/<hash>.json` (the commit point), syncs `meta/`, then removes `data/<version>` best effort (005). A data file left by a failed removal is an invisible orphan, reclaimed by crash recovery (feature 013).
