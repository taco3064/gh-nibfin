package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpAndInvalidCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		var out bytes.Buffer
		if err := run(args, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Bootstrap candidate") {
			t.Fatal("help must identify the unreleased baseline")
		}
	}
	for _, args := range [][]string{{"init"}, {"upgrade"}, {"help", "extra"}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatal("unsupported commands must not report success")
		}
	}
}
