package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
)

func readManagedIntent(path string) (managedInstallIntent, bool, error) {
	var intent managedInstallIntent
	file, err := privateInstallFile(path, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return intent, false, nil
	}
	if err != nil {
		return intent, false, err
	}
	defer file.Close()
	const limit = 2 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return intent, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > limit || decoder.Decode(&intent) != nil ||
		decoder.Decode(new(any)) != io.EOF ||
		uniqueUpgradeJSON(json.NewDecoder(bytes.NewReader(data))) != nil {
		return intent, false, errors.New("MANAGED_INSTALL_INTENT_CHANGED")
	}
	return intent, true, nil
}

func managedIntentUsesCurrentProducers(intent managedInstallIntent) bool {
	return reflect.DeepEqual(intent.Pins, Pins())
}

func validManagedIntent(intent managedInstallIntent, plan InitPlan) bool {
	if !validManagedIntentIdentity(intent, plan) {
		return false
	}
	if intent.Plan.NativeManaged && initIssueRecipe(intent.Plan) == "issue" {
		// Legal historical metadata is input to current observation, never proof
		// that the old package is authorized for a privileged installation.
		current := Pins()
		return intent.Plan.Execution != nil && validateProducerPins(intent.Pins) == nil &&
			intent.Pins.Core.Repository == current.Core.Repository &&
			intent.Pins.Action.Repository == current.Action.Repository
	}
	return managedIntentUsesCurrentProducers(intent)
}
