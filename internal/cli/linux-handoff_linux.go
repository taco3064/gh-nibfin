package cli

import (
	"errors"
	"flag"
	"strings"
)

func linuxIdentity() string {
	data, err := boundedLinuxFile("/etc/machine-id")
	id := strings.TrimSpace(string(data))
	if err != nil || !machineIdentity.MatchString(id) {
		return ""
	}
	return id
}

func (a App) linuxHandoffGuard(flags *flag.FlagSet) func() error {
	identity := flags.String("linux-target-identity", "", "Expected guest machine identity")
	arch := flags.String("linux-target-arch", "", "Expected native Linux architecture")
	var platform sharedPlatform
	verified := false
	return func() error {
		if *identity == "" && *arch == "" {
			return nil
		}
		if !machineIdentity.MatchString(*identity) || linuxIdentity() != *identity ||
			(*arch != "amd64" && *arch != "arm64") {
			return errors.New("GUEST_TARGET_CHANGED")
		}
		if !verified {
			var err error
			platform, err = verifiedSharedPlatform(a.API)
			if err != nil {
				return errors.New("HANDOFF_NATIVE_OBSERVATION_UNAVAILABLE")
			}
			verified = true
		}
		observation := probeLinuxTarget(platform, a.Process)
		if observation.Architecture != *arch {
			return errors.New("HANDOFF_NATIVE_ARCHITECTURE_CHANGED")
		}
		return requireLinuxObservation(observation)
	}
}
