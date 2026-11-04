// Package server runs the HTTP server and its graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ErrForcedShutdown is returned when in-flight requests did not finish within
// the shutdown timeout and their connections were closed.
var ErrForcedShutdown = errors.New("shutdown forced at deadline")

// Run serves h on ln until ctx is cancelled, then stops accepting
// connections, closes idle ones, and waits up to shutdownTimeout for active
// requests. It returns nil after a clean drain, an error wrapping
// ErrForcedShutdown when the deadline closed connections, or the Serve error
// if serving failed before ctx was cancelled.
func Run(ctx context.Context, ln net.Listener, h http.Handler, shutdownTimeout time.Duration, log *slog.Logger) error {
	srv := &http.Server{
		Handler: h,
		// Bounds slow-header clients. No ReadTimeout/WriteTimeout: object
		// bodies are streamed and get per-request deadlines instead.
		ReadHeaderTimeout: 10 * time.Second,
		// BaseContext stays the default so request contexts are not
		// cancelled by the shutdown signal; in-flight requests must finish.
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	began := time.Now()
	log.Info("shutdown_started", "cause", context.Cause(ctx).Error())

	// Derived from Background: ctx is already cancelled.
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		log.Warn("shutdown_forced", "elapsed", time.Since(began).String())
		return fmt.Errorf("%w: %v", ErrForcedShutdown, err)
	}
	log.Info("shutdown_complete", "elapsed", time.Since(began).String())
	return nil
}
