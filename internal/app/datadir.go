package app

import (
	"errors"
	"fmt"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

// checkDataDir creates dir if needed and proves it is writable by creating,
// closing and removing a probe file. MkdirAll alone also rejects a path that
// exists as a regular file (ENOTDIR), so no separate Stat is needed.
func checkDataDir(fsys fsx.FS, dir string) error {
	if err := fsys.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("data dir %s: %w", dir, err)
	}
	f, err := fsys.CreateTemp(dir, ".probe-*")
	if err != nil {
		return fmt.Errorf("data dir %s not writable: %w", dir, err)
	}
	// Remove the probe even when Close fails.
	if err := errors.Join(f.Close(), fsys.Remove(f.Name())); err != nil {
		return fmt.Errorf("data dir %s not writable: %w", dir, err)
	}
	return nil
}
