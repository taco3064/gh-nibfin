//go:build !linux

package cli

import "errors"

func overviewCore() (string, string, error) {
	return "", "", errors.New("UNSUPPORTED_LOCAL_OBSERVATION_PLATFORM")
}
