package cli

import (
	"bytes"
	"errors"
	"testing"
)

type unavailableVersionOutput struct{}

func (unavailableVersionOutput) Write([]byte) (int, error) {
	return 0, errors.New("unavailable")
}

func TestVersionWithoutProviders(t *testing.T) {
	previous := Version
	defer func() { Version = previous }()
	for _, version := range []string{"development", "1.2.3-rc.1+build.7"} {
		Version = version
		for _, command := range []string{"version", "--version"} {
			var out bytes.Buffer
			if err := (App{}).Run([]string{command}, &out); err != nil {
				t.Fatal(err)
			}
			if out.String() != "gh-nibfin "+version+"\n" {
				t.Fatal(out.String())
			}
		}
	}
	if err := (App{}).Run([]string{"version"}, unavailableVersionOutput{}); err == nil {
		t.Fatal("output failure ignored")
	}
	if err := (App{}).Run([]string{"version", "extra"}, &bytes.Buffer{}); err == nil {
		t.Fatal("extra argument accepted")
	}
}
