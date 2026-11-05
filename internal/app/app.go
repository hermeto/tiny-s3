// Package app wires configuration, the data directory, the listener and the
// HTTP server into the s3d process lifecycle. It holds every decision that
// cmd/s3d would otherwise make, so the coverage gate covers it.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/hermeto/tiny-s3/internal/config"
	"github.com/hermeto/tiny-s3/internal/fsx"
	"github.com/hermeto/tiny-s3/internal/s3api"
	"github.com/hermeto/tiny-s3/internal/server"
	"github.com/hermeto/tiny-s3/internal/storage"
)

// Main runs s3d with args (without the program name) until ctx is cancelled
// and returns the process exit code: 0 clean exit or -h, 1 runtime failure or
// forced shutdown, 2 usage error.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer, fsys fsx.FS) int {
	cfg, err := config.Parse(args, stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return 0
	case err != nil:
		return 2
	}
	return run(ctx, cfg, stdout, stderr, fsys, func(log *slog.Logger, store *storage.Store) http.Handler {
		ops := s3api.BucketOps(store, log)
		ops.PutObject = s3api.PutObjectOp(store, log)
		return s3api.NewHandler(ops, log)
	})
}

// run builds the logger, opens the store, then builds the handler from
// newHandler, so the handler can log as JSON on the same stream. The store is
// open, with every bucket loaded, before the port is.
func run(ctx context.Context, cfg config.Config, stdout, stderr io.Writer, fsys fsx.FS, newHandler func(*slog.Logger, *storage.Store) http.Handler) int {
	log := slog.New(slog.NewJSONHandler(stdout, nil))

	if err := checkDataDir(fsys, cfg.DataDir); err != nil {
		fmt.Fprintf(stderr, "s3d: %v\n", err)
		return 1
	}

	store, err := storage.Open(fsys, cfg.DataDir)
	if err != nil {
		fmt.Fprintf(stderr, "s3d: %v\n", err)
		return 1
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		fmt.Fprintf(stderr, "s3d: listen %s: %v\n", cfg.Listen, err)
		return 1
	}
	log.Info("listening", "listen", ln.Addr().String(), "data_dir", cfg.DataDir)

	if err := server.Run(ctx, ln, newHandler(log, store), cfg.ShutdownTimeout, log); err != nil {
		fmt.Fprintf(stderr, "s3d: %v\n", err)
		return 1
	}
	return 0
}
