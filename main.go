// Command gh-nibfin is the GitHub CLI extension for Nibfin.
package main

import (
	"os"

	"github.com/taco3064/gh-nibfin/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
