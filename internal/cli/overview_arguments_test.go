package cli

import (
	"io"
	"testing"
)

func TestOverviewTargetValidation(t *testing.T) {
	for _, host := range []string{"windows", "linux", "darwin"} {
		for _, target := range []string{"wsl:Ubuntu", "", "wsl:", "multipass:Ubuntu",
			"wsl:../Ubuntu", "wsl:A\nB"} {
			err := validateOverviewTarget(target, true, host)
			if (err == nil) != (host == "windows" && target == "wsl:Ubuntu") {
				t.Fatal(host, target, err)
			}
		}
	}
	for _, args := range [][]string{
		{"--target", "wsl:Ubuntu"}, {"--overview", "--target", ""},
		{"--overview", "--target", "multipass:Ubuntu"},
		{"--overview", "--target", "wsl:A\nB"},
		{"--overview", "--target"},
	} {
		if (App{}).Run(append([]string{"dashboard"}, args...), io.Discard) == nil {
			t.Fatal("public arguments accepted", args)
		}
	}
	for _, args := range [][]string{{"probe"}, {"probe", "0"},
		{"help", "1"}, {"observe", "1", "arbitrary"}} {
		if (App{}).Run(append([]string{overviewBridgeCommand}, args...), io.Discard) == nil {
			t.Fatal("public bridge accepted arbitrary command", args)
		}
	}
}
