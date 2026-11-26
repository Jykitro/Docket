// plandillo-cli calls Plandillo board tools from a shell; see internal/cli.
package main

import (
	"context"
	"os"
	"os/signal"

	"plandillo/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}
