// Package fsx is the filesystem layer every storage I/O call goes through.
// OS is the real implementation; Faulty wraps any FS to inject errors, so
// tests exercise I/O failure branches without mocking at call sites
// (constitution Principle IV). Later features extend FS with Rename, Sync
// and directory sync.
package fsx

import (
	"io/fs"
	"os"
)

// File is an open file.
type File interface {
	Name() string
	Close() error
}

// FS is the set of filesystem operations tiny-s3 uses.
type FS interface {
	MkdirAll(path string, perm fs.FileMode) error
	CreateTemp(dir, pattern string) (File, error)
	Remove(path string) error
}

// OS implements FS with the os package.
type OS struct{}

// MkdirAll calls os.MkdirAll.
func (OS) MkdirAll(path string, perm fs.FileMode) error { return os.MkdirAll(path, perm) }

// CreateTemp calls os.CreateTemp.
func (OS) CreateTemp(dir, pattern string) (File, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		// Return a nil interface, not a typed nil *os.File.
		return nil, err
	}
	return f, nil
}

// Remove calls os.Remove.
func (OS) Remove(path string) error { return os.Remove(path) }
