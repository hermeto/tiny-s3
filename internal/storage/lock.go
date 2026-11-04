package storage

import (
	"encoding/hex"
	"sync"
)

const lockStripes = 64

// keyLocks serializes commits, deletes and Get lookups per key. Keys are
// spread over a fixed number of mutexes so memory does not grow with the
// number of keys; unrelated keys sharing a stripe only wait for each other's
// short critical sections.
type keyLocks [lockStripes]sync.Mutex

// lock locks the stripe of a record name (64 hex characters) and returns the
// matching unlock.
func (l *keyLocks) lock(name string) func() {
	var b [1]byte
	_, _ = hex.Decode(b[:], []byte(name[:2]))
	m := &l[int(b[0])%lockStripes]
	m.Lock()
	return m.Unlock
}
