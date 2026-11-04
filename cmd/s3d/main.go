// Command s3d is the tiny-s3 server. It only wires process concerns (signals
// and the exit code); everything else lives in internal/app.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/hermeto/tiny-s3/internal/app"
	"github.com/hermeto/tiny-s3/internal/fsx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	// os.Exit skips deferred calls, so stop never runs: the signal
	// registration stays in place until exit and a second SIGTERM/SIGINT is
	// absorbed instead of killing the process mid-drain.
	defer stop()
	os.Exit(app.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, fsx.OS{}))
}
