package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/taco3064/gh-nibfin/internal/cli"
)

func interruptionContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func run(args []string, out io.Writer) error {
	ctx := context.Background()
	if len(args) > 0 && args[0] == "dashboard" {
		var stop context.CancelFunc
		ctx, stop = interruptionContext()
		defer stop()
	}
	process := cli.NativeProcess{}
	app := cli.App{API: cli.GitHub{Process: process}, Process: process,
		Runner: cli.NativeRunnerHost{}, Download: cli.OfficialRunnerDownload}
	return app.RunContext(ctx, args, out)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var commandError *cli.CommandError
		if errors.As(err, &commandError) {
			os.Exit(commandError.Exit)
		}
		os.Exit(1)
	}
}
