// Package config parses the s3d startup options.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

// DefaultShutdownTimeout is how long in-flight requests may drain after the
// first SIGTERM/SIGINT. It leaves a 1 s margin inside the 10 s stop budget for
// closing connections and exiting.
const DefaultShutdownTimeout = 9 * time.Second

// ErrUsage marks every invalid command line. Callers map it to exit code 2.
var ErrUsage = errors.New("usage error")

// Config holds the effective startup configuration.
type Config struct {
	Listen          string
	DataDir         string
	ShutdownTimeout time.Duration
}

// Parse parses args (without the program name). Usage and flag errors are
// written to stderr. -h and -help return flag.ErrHelp unwrapped; every other
// failure wraps ErrUsage.
func Parse(args []string, stderr io.Writer) (Config, error) {
	fs := flag.NewFlagSet("s3d", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := Config{ShutdownTimeout: DefaultShutdownTimeout}
	fs.StringVar(&cfg.Listen, "listen", ":9000", "TCP address to listen on (host:port)")
	fs.StringVar(&cfg.DataDir, "data-dir", "/data", "directory for object data; created if missing")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, flag.ErrHelp
		}
		return Config{}, fmt.Errorf("%w: %v", ErrUsage, err)
	}

	var problem string
	switch {
	case fs.NArg() > 0:
		problem = fmt.Sprintf("unexpected argument %q", fs.Arg(0))
	case cfg.Listen == "":
		problem = "-listen must not be empty"
	case cfg.DataDir == "":
		problem = "-data-dir must not be empty"
	}
	if problem != "" {
		fmt.Fprintln(stderr, problem)
		fs.Usage()
		return Config{}, fmt.Errorf("%w: %s", ErrUsage, problem)
	}

	cfg.DataDir = filepath.Clean(cfg.DataDir)
	return cfg, nil
}
