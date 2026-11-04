package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/taco3064/gh-nibfin/internal/cli"
)

func run(args []string, out io.Writer) error {
	process := cli.NativeProcess{}
	app := cli.App{API: cli.GitHub{Process: process}, Process: process}
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
