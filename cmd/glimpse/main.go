package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"glimpse/cmd/glimpse/command"
)

func main() {
	// SIGHUP arrives when the terminal closes.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if err := command.NewRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
