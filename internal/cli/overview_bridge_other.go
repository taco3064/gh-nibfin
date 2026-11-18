//go:build !linux

package cli

import (
	"errors"
	"io"
	"os"
)

func osOverviewInput() io.Reader { return os.Stdin }

func loadOverviewGuest() (overviewBinding, string, string, error) {
	return overviewBinding{}, "", "", errors.New("UNSUPPORTED_OVERVIEW_GUEST")
}
