package main

import (
	"fmt"
	"io"
	"os"
)

func run(args []string, out io.Writer) error {
	if len(args) == 0 || len(args) == 1 &&
		(args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "gh nibfin: manual Bootstrap candidate\n"+
			"Usage: gh nibfin help\n"+
			"Onboarding and Runner commands are not implemented yet.")
		return err
	}
	return fmt.Errorf("unknown command; use gh nibfin help")
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
