package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

const upgradeObserveCommand = "__upgrade-observe"

type upgradeGuestObservation struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Identity     string `json:"identity"`
	Architecture string `json:"architecture"`
	Installation string `json:"installation"`
	Adoption     string `json:"adoption"`
}

// The binding contains only the maintained nonsecret pool identity, never login
// material or an operation ledger. Pool equality remains owned by samePool.
func upgradeInstallation(host sharedHost) string {
	host.AdoptedRepositoryIDs = nil
	data, _ := json.Marshal(host)
	return base64.RawURLEncoding.EncodeToString(data)
}

func matchesUpgradeInstallation(value string, host *sharedHost) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	planned, err := decodeSharedHost(data)
	return err == nil && planned.samePool(host)
}

func (a App) runUpgradeObservation(args []string, out io.Writer) error {
	if len(args) != 1 || !repoPattern.MatchString(args[0]) {
		return errors.New("INVALID_UPGRADE_OBSERVATION_ARGUMENTS")
	}
	var observed upgradeGuestObservation
	var err error
	if a.upgradeObservation != nil {
		observed, err = a.upgradeObservation(args[0])
	} else {
		observed, err = a.observeInstalledUpgrade(args[0])
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(observed)
}

func upgradeHostObservation(o upgradeGuestObservation, host *sharedHost,
	id int64) upgradeGuestObservation {
	o.RepositoryID = id
	o.Adoption = host.adoptionStatus(id)
	o.Installation = upgradeInstallation(*host)
	return o
}
