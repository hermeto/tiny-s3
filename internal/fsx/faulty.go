package fsx

import (
	"io/fs"
	"strings"
)

// Operation names for Fault.Op.
const (
	OpMkdirAll   = "mkdirall"
	OpCreateTemp = "createtemp"
	OpClose      = "close"
	OpRemove     = "remove"
)

// Fault makes an operation fail with Err. It matches when Op equals the
// operation and the operation's path starts with PathPrefix (an empty prefix
// matches every path). For CreateTemp the path is the directory; for Close it
// is the file name.
type Fault struct {
	Op         string
	PathPrefix string
	Err        error
}

// Faulty wraps FS and returns the first matching fault's error instead of
// performing the operation. Non-matching calls go to FS unchanged.
type Faulty struct {
	FS     FS
	Faults []Fault
}

func (f Faulty) fault(op, path string) error {
	for _, ft := range f.Faults {
		if ft.Op == op && strings.HasPrefix(path, ft.PathPrefix) {
			return ft.Err
		}
	}
	return nil
}

// MkdirAll implements FS.
func (f Faulty) MkdirAll(path string, perm fs.FileMode) error {
	if err := f.fault(OpMkdirAll, path); err != nil {
		return err
	}
	return f.FS.MkdirAll(path, perm)
}

// CreateTemp implements FS. The returned file applies close faults.
func (f Faulty) CreateTemp(dir, pattern string) (File, error) {
	if err := f.fault(OpCreateTemp, dir); err != nil {
		return nil, err
	}
	file, err := f.FS.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return faultyFile{File: file, faults: f}, nil
}

// Remove implements FS.
func (f Faulty) Remove(path string) error {
	if err := f.fault(OpRemove, path); err != nil {
		return err
	}
	return f.FS.Remove(path)
}

type faultyFile struct {
	File
	faults Faulty
}

// Close closes the underlying file (so no descriptor leaks) and then reports
// the injected error, if any.
func (ff faultyFile) Close() error {
	err := ff.File.Close()
	if ferr := ff.faults.fault(OpClose, ff.Name()); ferr != nil {
		return ferr
	}
	return err
}
