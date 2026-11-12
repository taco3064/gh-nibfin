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

func run(args []string, out io.Writer) error {
	process := cli.NativeProcess{}
	app := cli.App{API: cli.GitHub{Process: process}, Process: process,
		Runner: cli.NativeRunnerHost{}, Download: cli.OfficialRunnerDownload}
	return runApp(app, args, out)
}

func runApp(app cli.App, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "dashboard" {
		ctx, stop := signal.NotifyContext(context.Background(),
			os.Interrupt, syscall.SIGTERM)
		defer stop()
		app.Context = ctx
	}
	return app.Run(args, out)
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
