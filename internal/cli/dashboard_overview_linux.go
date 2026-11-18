package cli

import "errors"

func overviewCore() (string, string, error) {
	if !nativeSharedHostSupported() {
		return "", "", errors.New("UNSUPPORTED_LOCAL_OBSERVATION_PLATFORM")
	}
	return trustedSharedCore()
}
