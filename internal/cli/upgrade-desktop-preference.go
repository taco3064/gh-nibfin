package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type upgradePreference struct {
	Target      string                  `json:"target"`
	Observation upgradeGuestObservation `json:"observation"`
}

func (a App) desktopUpgradePreferencePath() (string, error) {
	if a.upgradePreferencePath != nil {
		return a.upgradePreferencePath()
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "nibfin", "upgrade-targets.json"), nil
}

func (a App) readUpgradePreferences() map[string]upgradePreference {
	values := map[string]upgradePreference{}
	path, err := a.desktopUpgradePreferencePath()
	if err != nil {
		return values
	}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &values)
	}
	if values == nil {
		values = map[string]upgradePreference{}
	}
	return values
}

func (a App) rememberUpgradeTarget(repo string, selected upgradeDesktopTarget) error {
	path, err := a.desktopUpgradePreferencePath()
	if err != nil {
		return err
	}
	values := a.readUpgradePreferences()
	values[strings.ToLower(repo)] = upgradePreference{
		Target: "wsl:" + selected.target.Name, Observation: selected.observed,
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".upgrade-target-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return errors.New("UPGRADE_TARGET_PREFERENCE_WRITE_UNAVAILABLE")
	}
	return nil
}

func sameUpgradeObservation(a, b upgradeGuestObservation) bool {
	data, err := decodeUpgradePool(a.Installation)
	return err == nil && a.Identity == b.Identity && a.Architecture == b.Architecture &&
		a.RepositoryID == b.RepositoryID && matchesUpgradeInstallation(b.Installation, data)
}
