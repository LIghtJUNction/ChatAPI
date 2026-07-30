package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/zyf2007/ChatAPI/internal/bootstrap"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "chatapi server: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	mode, err := bootstrap.ModeFromArgs(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// Restore the default signal behavior so a second Ctrl+C forces exit.
		stop()
	}()

	app, err := bootstrap.New(ctx, bootstrap.Options{Mode: mode})
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
