package cli

import (
	"encoding/json"
	"errors"
)

func decodeUpgradeExit(data []byte) error {
	var result struct {
		Exit         *int `json:"guest_exit"`
		LaunchFailed bool `json:"launch_failed"`
	}
	if json.Unmarshal(data, &result) != nil {
		return errors.New("GUEST_TRANSPORT_LOST_READ_BACK_REQUIRED")
	}
	if result.LaunchFailed {
		return errors.New("GUEST_LAUNCH_FAILED")
	}
	if result.Exit == nil {
		return errors.New("GUEST_LAUNCH_OR_TRANSPORT_UNAVAILABLE_READ_BACK_REQUIRED")
	}
	return guestUpgradeExit(*result.Exit)
}
