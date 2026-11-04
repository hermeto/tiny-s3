package fsx

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
)

// Operation names for Fault.Op and Record.Op.
const (
	OpMkdirAll   = "mkdirall"
	OpCreate     = "create"
	OpCreateTemp = OpCreate // compatibility name for the original data-dir seam
	OpWrite      = "write"
	OpSync       = "sync"
	OpRename     = "rename"
	OpRemove     = "remove"
	OpOpenDir    = "opendir"
	OpDirSync    = "dirsync"
	OpClose      = "close"
)

// Fault makes an operation fail or return a short write on the Nth matching
// call. PathPattern uses path.Match; PathPrefix remains supported for the
// original prefix-based test seam.
type Fault struct {
	Op          string
	PathPattern string
	PathPrefix  string
	Nth         int
	Err         error
	WriteCount  int
}

// Record is one attempted operation in the order it was made.
type Record struct {
	Op        string
	Path      string
	OtherPath string
	Requested int
	Written   int
	Err       error
}

type faultyState struct {
	mu      sync.Mutex
	counts  []int
	history []Record
}

// Faulty wraps FS and injects configured faults. Use NewFaulty so glob
// patterns are validated and state is shared by all returned handles.
type Faulty struct {
	FS     FS
	Faults []Fault
	state  *faultyState
}

func (f *Faulty) ensureState() {
	if f.state == nil {
		f.state = &faultyState{counts: make([]int, len(f.Faults))}
	}
}

// NewFaulty creates a stateful fault-injecting filesystem. Invalid glob
// patterns panic because a malformed test rule is a programmer error.
func NewFaulty(fsys FS, faults ...Fault) *Faulty {
	for _, fault := range faults {
		pattern := fault.PathPattern
		if pattern != "" {
			if _, err := path.Match(pattern, ""); err != nil {
				panic(fmt.Sprintf("invalid filesystem fault pattern %q: %v", pattern, err))
			}
		}
	}
	return &Faulty{FS: fsys, Faults: append([]Fault(nil), faults...), state: &faultyState{counts: make([]int, len(faults))}}
}

// History returns a copy of the attempted operation history.
func (f *Faulty) History() []Record {
	f.ensureState()
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	return append([]Record(nil), f.state.history...)
}

func (f *Faulty) match(fault Fault, value string) bool {
	if fault.PathPattern != "" {
		matched, err := path.Match(fault.PathPattern, value)
		return err == nil && matched
	}
	return strings.HasPrefix(value, fault.PathPrefix)
}

func (f *Faulty) begin(op, pathValue, otherPath string, requested int) (int, Fault, bool) {
	f.ensureState()
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	index := len(f.state.history)
	f.state.history = append(f.state.history, Record{Op: op, Path: pathValue, OtherPath: otherPath, Requested: requested})
	for i, fault := range f.Faults {
		if fault.Op != op || !f.match(fault, pathValue) {
			continue
		}
		f.state.counts[i]++
		nth := fault.Nth
		if nth <= 0 {
			nth = 1
		}
		if f.state.counts[i] == nth {
			return index, fault, true
		}
		break
	}
	return index, Fault{}, false
}

func (f *Faulty) finish(index, written int, err error) {
	f.state.mu.Lock()
	f.state.history[index].Written = written
	f.state.history[index].Err = err
	f.state.mu.Unlock()
}

// MkdirAll implements FS.
func (f *Faulty) MkdirAll(pathValue string, perm fs.FileMode) error {
	index, fault, inject := f.begin(OpMkdirAll, pathValue, "", 0)
	var err error
	if inject {
		err = fault.Err
	} else {
		err = f.FS.MkdirAll(pathValue, perm)
	}
	f.finish(index, 0, err)
	return err
}

// CreateTemp implements FS and wraps the returned file handle.
func (f *Faulty) CreateTemp(dir, pattern string) (File, error) {
	index, fault, inject := f.begin(OpCreate, dir, "", 0)
	if inject {
		f.finish(index, 0, fault.Err)
		return nil, fault.Err
	}
	file, err := f.FS.CreateTemp(dir, pattern)
	f.finish(index, 0, err)
	if err != nil {
		return nil, err
	}
	return &faultyFile{File: file, owner: f}, nil
}

// OpenDir implements FS and wraps the returned directory handle.
func (f *Faulty) OpenDir(pathValue string) (Dir, error) {
	index, fault, inject := f.begin(OpOpenDir, pathValue, "", 0)
	if inject {
		f.finish(index, 0, fault.Err)
		return nil, fault.Err
	}
	dir, err := f.FS.OpenDir(pathValue)
	f.finish(index, 0, err)
	if err != nil {
		return nil, err
	}
	return &faultyDir{Dir: dir, owner: f}, nil
}

// Rename implements FS.
func (f *Faulty) Rename(oldPath, newPath string) error {
	index, fault, inject := f.begin(OpRename, oldPath, newPath, 0)
	var err error
	if inject {
		err = fault.Err
	} else {
		err = f.FS.Rename(oldPath, newPath)
	}
	f.finish(index, 0, err)
	return err
}

// Remove implements FS.
func (f *Faulty) Remove(pathValue string) error {
	index, fault, inject := f.begin(OpRemove, pathValue, "", 0)
	var err error
	if inject {
		err = fault.Err
	} else {
		err = f.FS.Remove(pathValue)
	}
	f.finish(index, 0, err)
	return err
}

// WrapFile applies this fake's rules to an existing file handle.
func (f *Faulty) WrapFile(file File) File { return &faultyFile{File: file, owner: f} }

// WrapDir applies this fake's rules to an existing directory handle.
func (f *Faulty) WrapDir(dir Dir) Dir { return &faultyDir{Dir: dir, owner: f} }

type faultyFile struct {
	File
	owner *Faulty
}

func (f *faultyFile) Write(p []byte) (int, error) {
	index, fault, inject := f.owner.begin(OpWrite, f.Name(), "", len(p))
	if inject {
		if fault.WriteCount > 0 && fault.WriteCount < len(p) {
			f.owner.finish(index, fault.WriteCount, fault.Err)
			if fault.Err == nil {
				return fault.WriteCount, io.ErrShortWrite
			}
			return fault.WriteCount, fault.Err
		}
		f.owner.finish(index, 0, fault.Err)
		return 0, fault.Err
	}
	n, err := f.File.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	f.owner.finish(index, n, err)
	return n, err
}

func (f *faultyFile) Sync() error {
	index, fault, inject := f.owner.begin(OpSync, f.Name(), "", 0)
	var err error
	if inject {
		err = fault.Err
	} else {
		err = f.File.Sync()
	}
	f.owner.finish(index, 0, err)
	return err
}

func (f *faultyFile) Close() error {
	index, fault, inject := f.owner.begin(OpClose, f.Name(), "", 0)
	err := f.File.Close()
	if inject && fault.Err != nil {
		err = fault.Err
	}
	f.owner.finish(index, 0, err)
	return err
}

type faultyDir struct {
	Dir
	owner *Faulty
}

func (d *faultyDir) Sync() error {
	index, fault, inject := d.owner.begin(OpDirSync, d.Name(), "", 0)
	var err error
	if inject {
		err = fault.Err
	} else {
		err = d.Dir.Sync()
	}
	d.owner.finish(index, 0, err)
	return err
}

func (d *faultyDir) Close() error {
	index, fault, inject := d.owner.begin(OpClose, d.Name(), "", 0)
	err := d.Dir.Close()
	if inject && fault.Err != nil {
		err = fault.Err
	}
	d.owner.finish(index, 0, err)
	return err
}
