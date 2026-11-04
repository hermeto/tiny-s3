package config_test

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/hermeto/tiny-s3/internal/config"
)

func TestParseValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want config.Config
	}{
		{
			name: "defaults",
			args: nil,
			want: config.Config{Listen: ":9000", DataDir: "/data", ShutdownTimeout: 9 * time.Second},
		},
		{
			name: "overrides with cleaned data dir",
			args: []string{"-listen=127.0.0.1:9100", "-data-dir=/tmp/x/../y"},
			want: config.Config{Listen: "127.0.0.1:9100", DataDir: "/tmp/y", ShutdownTimeout: 9 * time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			got, err := config.Parse(tt.args, &stderr)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr output: %q", stderr.String())
			}
		})
	}
}

func TestParseUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown flag", args: []string{"-bogus"}},
		{name: "missing value", args: []string{"-listen"}},
		{name: "empty listen", args: []string{"-listen="}},
		{name: "empty data dir", args: []string{"-data-dir="}},
		{name: "positional argument", args: []string{"extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			_, err := config.Parse(tt.args, &stderr)
			if !errors.Is(err, config.ErrUsage) {
				t.Fatalf("Parse(%q) error = %v, want ErrUsage", tt.args, err)
			}
			if !strings.Contains(stderr.String(), "Usage of s3d") {
				t.Fatalf("usage not written to stderr: %q", stderr.String())
			}
		})
	}
}

func TestParseHelp(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"-h", "-help"} {
		t.Run(arg, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			_, err := config.Parse([]string{arg}, &stderr)
			// Compare directly: ErrHelp must be returned unwrapped.
			if err != flag.ErrHelp {
				t.Fatalf("Parse(%q) error = %v, want flag.ErrHelp", arg, err)
			}
			if !strings.Contains(stderr.String(), "Usage of s3d") {
				t.Fatalf("usage not written to stderr: %q", stderr.String())
			}
		})
	}
}
