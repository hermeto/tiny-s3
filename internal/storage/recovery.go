package storage

import (
	"fmt"
	"path/filepath"
)

// RecoveryReport is what Open did to bring the store to a consistent state.
type RecoveryReport struct {
	// TmpRemoved counts leftovers of interrupted writes removed from tmp/.
	TmpRemoved int
	// OrphansRemoved counts bodies in data/ that no valid record names.
	OrphansRemoved int
	// ObjectsIndexed counts the records loaded into the index.
	ObjectsIndexed int
	// RecordsSkipped counts records left out of the index: undecodable,
	// stored under a file name that is not their key's hash, or whose body
	// is missing.
	RecordsSkipped int
	// Buckets counts the buckets loaded.
	Buckets int
}

// Recovery returns what Open did.
func (s *Store) Recovery() RecoveryReport { return s.recovery }

// listBodies returns the names of the files in data/.
func (s *Store) listBodies() (map[string]bool, error) {
	dir := filepath.Join(s.root, dataDir)
	des, err := s.fsys.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: list %s: %w", dir, err)
	}
	bodies := make(map[string]bool, len(des))
	for _, de := range des {
		bodies[de.Name()] = true
	}
	return bodies, nil
}

// removeOrphans removes every body that no indexed record names: bodies of a
// write that crashed before its record was committed, previous versions whose
// removal did not happen, and bodies of records that cannot be read.
func (s *Store) removeOrphans(bodies, referenced map[string]bool) error {
	for name := range bodies {
		if referenced[name] {
			continue
		}
		path := s.dataPath(name)
		if err := s.fsys.Remove(path); err != nil {
			return fmt.Errorf("storage: remove orphan %s: %w", path, err)
		}
		s.recovery.OrphansRemoved++
	}
	return nil
}
