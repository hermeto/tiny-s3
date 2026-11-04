package storage

import (
	"errors"
	"fmt"
)

// Delete removes bucket/key. It succeeds when the key does not exist, as S3
// DeleteObject does. The record removal is synced before Delete returns; the
// data file is removed afterwards, best effort, so a failed sync never leaves
// a record that could reappear after a crash without its body.
func (s *Store) Delete(bucket, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	name := recordName(bucket, key)
	unlock := s.locks.lock(name)
	defer unlock()

	rec, err := s.readRecord(name)
	switch {
	case errors.Is(err, ErrNoSuchKey):
		return nil
	case errors.Is(err, ErrCorruptRecord):
		// Remove the unreadable record; its data, if any, cannot be found.
		rec = record{}
	case err != nil:
		return err
	}
	if err := s.fsys.Remove(s.recordPath(name)); err != nil {
		return fmt.Errorf("storage: remove record: %w", err)
	}
	if err := s.syncDir(metaDir); err != nil {
		return err
	}
	if rec.Data != "" {
		_ = s.fsys.Remove(s.dataPath(rec.Data))
	}
	return nil
}
