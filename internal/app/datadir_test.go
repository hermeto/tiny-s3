package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hermeto/tiny-s3/internal/fsx"
)

func TestMainCreatesMissingDataDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b")
	r := launch(t, func(ctx context.Context, stdout, stderr io.Writer) int {
		return Main(ctx, []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, stdout, stderr, fsx.OS{})
	})
	r.waitFor(t, "listening")

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o750 {
		t.Errorf("data dir mode = %v, want directory with 0750", info.Mode())
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("data dir entries = %v (%v), want none", entries, err)
	}

	r.cancel()
	if c := r.code(t); c != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", c, r.stderr.String())
	}
}

func TestMainDataDirFailures(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")

	tests := []struct {
		name string
		// setup returns the data dir and the FS to use.
		setup      func(t *testing.T) (string, fsx.FS)
		wantStderr func(dir string) string
		probeGone  bool // the probe file must have been removed
	}{
		{
			name: "path is a regular file",
			setup: func(t *testing.T) (string, fsx.FS) {
				path := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return path, fsx.OS{}
			},
			wantStderr: func(dir string) string {
				return "s3d: data dir " + dir + ": mkdir " + dir + ": not a directory\n"
			},
		},
		{
			name: "mkdirall fails",
			setup: func(t *testing.T) (string, fsx.FS) {
				return t.TempDir(), &fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: fsx.OpMkdirAll, Err: errBoom}}}
			},
			wantStderr: func(dir string) string { return "s3d: data dir " + dir + ": boom\n" },
		},
		{
			name: "createtemp fails",
			setup: func(t *testing.T) (string, fsx.FS) {
				return t.TempDir(), &fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: fsx.OpCreateTemp, Err: errBoom}}}
			},
			wantStderr: func(dir string) string { return "s3d: data dir " + dir + " not writable: boom\n" },
		},
		{
			name: "close fails",
			setup: func(t *testing.T) (string, fsx.FS) {
				return t.TempDir(), &fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: fsx.OpClose, Err: errBoom}}}
			},
			wantStderr: func(dir string) string { return "s3d: data dir " + dir + " not writable: boom\n" },
			probeGone:  true,
		},
		{
			name: "remove fails",
			setup: func(t *testing.T) (string, fsx.FS) {
				return t.TempDir(), &fsx.Faulty{FS: fsx.OS{}, Faults: []fsx.Fault{{Op: fsx.OpRemove, Err: errBoom}}}
			},
			wantStderr: func(dir string) string { return "s3d: data dir " + dir + " not writable: boom\n" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, fsys := tt.setup(t)
			var stdout, stderr bytes.Buffer

			c := Main(context.Background(), []string{"-listen=127.0.0.1:0", "-data-dir=" + dir}, &stdout, &stderr, fsys)

			if c != 1 {
				t.Fatalf("exit = %d, want 1", c)
			}
			if want := tt.wantStderr(dir); stderr.String() != want {
				t.Errorf("stderr = %q, want %q", stderr.String(), want)
			}
			if strings.Contains(stdout.String(), "listening") {
				t.Errorf("server listened despite the failure: %q", stdout.String())
			}
			if tt.probeGone {
				if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
					t.Errorf("probe left behind: %v (%v)", entries, err)
				}
			}
		})
	}
}

func TestMainEmptyDataDirIsUsageError(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if c := Main(context.Background(), []string{"-data-dir="}, &stdout, &stderr, fsx.OS{}); c != 2 {
		t.Fatalf("exit = %d, want 2", c)
	}
}
