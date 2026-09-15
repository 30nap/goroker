// Command goroker is a browser-automation assistant for an Iranian stock
// brokerage account. It never submits an order without an explicit interactive
// confirmation.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/30nap/goroker/cmd"
)

func main() {
	// Ctrl+C and SIGTERM cancel the context, which stops monitoring, closes
	// the browser and aborts anything in flight. Nothing is ever submitted
	// after cancellation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cmd.Execute(ctx))
}
