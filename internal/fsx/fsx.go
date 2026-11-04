// Package fsx is the filesystem layer every storage I/O call goes through.
package fsx

import (
	"io/fs"
	"os"
)

// File is an open regular file.
type File interface {
	Name() string
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// Dir is an open directory.
type Dir interface {
	Name() string
	Sync() error
	Close() error
}

// FS is the set of filesystem operations tiny-s3 uses.
type FS interface {
	MkdirAll(path string, perm fs.FileMode) error
	CreateTemp(dir, pattern string) (File, error)
	OpenDir(path string) (Dir, error)
	Rename(oldPath, newPath string) error
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
		return nil, err
	}
	return f, nil
}

// OpenDir calls os.Open for a directory.
func (OS) OpenDir(path string) (Dir, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Rename calls os.Rename.
func (OS) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

// Remove calls os.Remove.
func (OS) Remove(path string) error { return os.Remove(path) }
