package cli

import (
	"encoding/json"
	"errors"
)

type relayObservation struct {
	Status          string `json:"status"`
	ServicesStarted *bool  `json:"servicesStarted"`
	Resources       []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"resources"`
	Github struct {
		Status      string             `json:"status"`
		Controller  struct{ ID int64 } `json:"controller"`
		Origins     []relayOrigin      `json:"origins"`
		Destination struct {
			Repository   string `json:"repository"`
			RepositoryID int64  `json:"repositoryId"`
		} `json:"destination"`
	} `json:"github"`
	Control *struct {
		Version int   `json:"schemaVersion"`
		Paused  *bool `json:"paused"`
	} `json:"control"`
	Recovery struct {
		Status  string `json:"status"`
		Control *struct {
			Version int   `json:"schemaVersion"`
			Paused  *bool `json:"paused"`
		} `json:"control"`
		Records []struct {
			Origin struct {
				RepositoryID int64 `json:"repositoryId"`
			} `json:"origin"`
			Status string `json:"status"`
			Code   string `json:"code"`
		} `json:"records"`
	} `json:"recovery"`
	Service struct {
		Status string `json:"status"`
		Units  []struct {
			ID            string `json:"Id"`
			LoadState     string `json:"LoadState"`
			ActiveState   string `json:"ActiveState"`
			UnitFileState string `json:"UnitFileState"`
		} `json:"units"`
	} `json:"service"`
}

type managedRelayResult struct {
	Mode             string `json:"mode"`
	Service          string `json:"service"`
	Control          string `json:"control"`
	Github           string `json:"github"`
	Recovery         string `json:"recovery"`
	Consumed         int    `json:"consumed"`
	UnknownEffects   int    `json:"unknown_effects"`
	DispatchObserved bool   `json:"dispatch_observed"`
}

func decodeRelayObservation(data []byte) (relayObservation, error) {
	var result relayObservation
	if json.Unmarshal(data, &result) != nil ||
		(result.Status != "inspected" && result.Status != "prepared") {
		return result, errors.New("MANAGED_RELAY_INSPECTION_REQUIRED")
	}
	return result, nil
}

func (o relayObservation) paused() (bool, error) {
	control := o.Control
	if o.Status == "inspected" {
		control = o.Recovery.Control
	}
	if control == nil || control.Version != 1 || control.Paused == nil {
		return false, errors.New("MANAGED_RELAY_CONTROL_UNVERIFIED")
	}
	return *control.Paused, nil
}

func (o relayObservation) verify(config relayConfiguration, plan InitPlan) error {
	if o.Status == "inspected" &&
		(o.Recovery.Status != "observed" || len(o.Recovery.Records) > 4096) {
		return errors.New("MANAGED_RELAY_RECOVERY_INSPECTION_REQUIRED")
	}
	if o.Github.Status != "verified" || o.Github.Controller.ID != plan.Member.ID ||
		plan.Execution == nil ||
		o.Github.Destination.Repository != plan.Execution.Repository ||
		o.Github.Destination.RepositoryID != plan.Execution.RepositoryID ||
		len(o.Github.Origins) != len(config.Origins) ||
		o.Service.Status != "observed" || len(o.Service.Units) != 2 {
		return errors.New("MANAGED_RELAY_READINESS_UNVERIFIED")
	}
	for _, origin := range config.Origins {
		matches := 0
		for _, observed := range o.Github.Origins {
			if observed == origin {
				matches++
			}
		}
		if matches != 1 {
			return errors.New("MANAGED_RELAY_BINDINGS_CHANGED")
		}
	}
	if len(o.Resources) != 2 {
		return errors.New("MANAGED_RELAY_RESOURCES_UNVERIFIED")
	}
	for _, name := range []string{"nibfin-notification-relay.service",
		"nibfin-notification-relay.timer"} {
		resources, units := 0, 0
		for _, resource := range o.Resources {
			if resource.Name == name &&
				(resource.Status == "prepared" || resource.Status == "unchanged") {
				resources++
			}
		}
		for _, unit := range o.Service.Units {
			if unit.ID == name && unit.LoadState == "loaded" &&
				(unit.ActiveState == "active" || unit.ActiveState == "inactive") {
				units++
			}
		}
		if resources != 1 || units != 1 {
			return errors.New("MANAGED_RELAY_SERVICE_UNVERIFIED")
		}
	}
	return nil
}

func (o relayObservation) configured(targetID int64) (managedRelayResult, error) {
	result := managedRelayResult{Mode: "managed", Service: "observed",
		Control: "resumed", Github: "verified", Recovery: "observed"}
	paused, err := o.paused()
	if err != nil || paused || o.Recovery.Status != "observed" ||
		len(o.Recovery.Records) > 4096 {
		return result, errors.New("MANAGED_RELAY_RECOVERY_INSPECTION_REQUIRED")
	}
	enabled := false
	for _, unit := range o.Service.Units {
		if unit.ID == "nibfin-notification-relay.timer" &&
			unit.LoadState == "loaded" && unit.ActiveState == "active" &&
			unit.UnitFileState == "enabled" {
			enabled = true
		}
	}
	if !enabled {
		return result, errors.New("MANAGED_RELAY_TIMER_UNVERIFIED")
	}
	for _, record := range o.Recovery.Records {
		if record.Status == "consumed" {
			result.Consumed++
			result.DispatchObserved = result.DispatchObserved ||
				record.Origin.RepositoryID == targetID
		}
		if record.Code == "RELAY_EFFECT_UNKNOWN" {
			result.UnknownEffects++
		}
	}
	return result, nil
}
