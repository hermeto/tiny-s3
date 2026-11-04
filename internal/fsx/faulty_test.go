package fsx_test

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

func TestFaultyInjectsNthMatchingCallAndRecordsHistory(t *testing.T) {
	dir := t.TempDir()
	errInjected := errors.New("injected")
	fsys := fsx.NewFaulty(fsx.OS{}, fsx.Fault{
		Op:          fsx.OpMkdirAll,
		PathPattern: filepath.Join(dir, "match-*"),
		Nth:         2,
		Err:         errInjected,
	})

	if err := fsys.MkdirAll(filepath.Join(dir, "other"), 0o750); err != nil {
		t.Fatalf("nonmatching mkdir: %v", err)
	}
	if err := fsys.MkdirAll(filepath.Join(dir, "match-a"), 0o750); err != nil {
		t.Fatalf("first matching mkdir: %v", err)
	}
	if err := fsys.MkdirAll(filepath.Join(dir, "match-b"), 0o750); !errors.Is(err, errInjected) {
		t.Fatalf("second matching mkdir: %v", err)
	}

	history := fsys.History()
	if len(history) != 3 || history[0].Path != filepath.Join(dir, "other") || history[2].Err == nil {
		t.Fatalf("history = %#v", history)
	}
	history[0].Path = "mutated"
	if fsys.History()[0].Path == "mutated" {
		t.Fatal("History returned mutable internal state")
	}
}

func TestFaultyInjectsEveryOperation(t *testing.T) {
	errInjected := errors.New("injected")
	dir := t.TempDir()
	base := fsx.OS{}
	faults := []fsx.Fault{
		{Op: fsx.OpCreate, Err: errInjected},
		{Op: fsx.OpWrite, Err: errInjected},
		{Op: fsx.OpSync, Err: errInjected},
		{Op: fsx.OpRename, Err: errInjected},
		{Op: fsx.OpRemove, Err: errInjected},
		{Op: fsx.OpMkdirAll, Err: errInjected},
		{Op: fsx.OpOpenDir, Err: errInjected},
		{Op: fsx.OpDirSync, Err: errInjected},
	}
	for _, fault := range faults {
		t.Run(fault.Op, func(t *testing.T) {
			fsys := fsx.NewFaulty(base, fault)
			switch fault.Op {
			case fsx.OpCreate:
				_, err := fsys.CreateTemp(dir, "x-*")
				if !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			case fsx.OpWrite, fsx.OpSync:
				f, err := base.CreateTemp(dir, "x-*")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if fault.Op == fsx.OpWrite {
					_, err = fsys.WrapFile(f).Write([]byte("x"))
				} else {
					err = fsys.WrapFile(f).Sync()
				}
				if !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			case fsx.OpRename:
				if err := fsys.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "b")); !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			case fsx.OpRemove:
				if err := fsys.Remove(filepath.Join(dir, "a")); !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			case fsx.OpMkdirAll:
				if err := fsys.MkdirAll(filepath.Join(dir, "a"), 0o750); !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			case fsx.OpOpenDir:
				if _, err := fsys.OpenDir(dir); !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			case fsx.OpDirSync:
				d, err := base.OpenDir(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				if err := fsys.WrapDir(d).Sync(); !errors.Is(err, errInjected) {
					t.Fatalf("err = %v", err)
				}
			}
		})
	}
}

func TestFaultyShortWrite(t *testing.T) {
	dir := t.TempDir()
	f, err := fsx.OS{}.CreateTemp(dir, "x-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fsys := fsx.NewFaulty(fsx.OS{}, fsx.Fault{Op: fsx.OpWrite, WriteCount: 1})
	n, err := fsys.WrapFile(f).Write([]byte("abc"))
	if n != 1 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write = (%d, %v), want short write", n, err)
	}
}

func TestFaultyRecordsDurabilityOrder(t *testing.T) {
	dir := t.TempDir()
	fsys := fsx.NewFaulty(fsx.OS{})
	f, err := fsys.CreateTemp(dir, "x-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "final")
	if err := fsys.Rename(name, final); err != nil {
		t.Fatal(err)
	}
	d, err := fsys.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	defer fsys.Remove(final)

	var got []string
	for _, record := range fsys.History() {
		got = append(got, record.Op)
	}
	want := []string{fsx.OpCreate, fsx.OpWrite, fsx.OpSync, fsx.OpClose, fsx.OpRename, fsx.OpOpenDir, fsx.OpDirSync, fsx.OpClose}
	if len(got) != len(want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("operations = %v, want %v", got, want)
		}
	}
}

func TestFaultyInvalidPatternPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewFaulty did not reject invalid pattern")
		}
	}()
	_ = fsx.NewFaulty(fsx.OS{}, fsx.Fault{Op: fsx.OpRemove, PathPattern: "["})
}

func TestFaultyHistoryAndRulesAreRaceSafe(t *testing.T) {
	fsys := fsx.NewFaulty(fsx.OS{})
	dir := t.TempDir()
	const workers = 8
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_ = fsys.MkdirAll(filepath.Join(dir, "same"), 0o750)
			done <- struct{}{}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}
	if got := len(fsys.History()); got != workers {
		t.Fatalf("history length = %d, want %d", got, workers)
	}
}
