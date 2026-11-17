//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"errors"
)

const managedHostEntry = "bin/nibfin-prepare-managed-onboarding.mjs"

type preparationRepository struct {
	Repository string `json:"repository"`
	ID         int64  `json:"repositoryId"`
}

type managedHostPreparation struct {
	Version      int                     `json:"schemaVersion"`
	MemberID     int64                   `json:"memberId"`
	Pin          SourcePin               `json:"pin"`
	Repositories []preparationRepository `json:"repositories"`
}

func prepareManagedHost(process Process, plan InitPlan) error {
	input, err := json.Marshal(managedHostPreparation{
		Version: 1, MemberID: plan.Member.ID, Pin: Pins().Core,
		Repositories: []preparationRepository{
			{plan.Execution.Repository, plan.Execution.RepositoryID},
			{plan.Repository.FullName, plan.Repository.ID},
		},
	})
	if err != nil {
		return err
	}
	const core = "/opt/nibfin/core"
	node, _, err := trustedSharedCore()
	if err != nil {
		return err
	}
	if err := verifyPrivilegedCoreTree(core); err != nil {
		return err
	}
	entry := core + "/" + managedHostEntry
	if err := requireRootOwnedHostPath(entry); err != nil {
		return err
	}
	output, err := runSharedAdministrator(process, "/usr/bin/env",
		[]string{"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", node, entry,
			"--consent-dedicated-engine"}, input)
	var result struct {
		Status        string `json:"status"`
		Adoption      bool   `json:"adoption"`
		ServicesReady bool   `json:"servicesReady"`
	}
	if err != nil || json.Unmarshal(output, &result) != nil ||
		result.Status != "prepared" || result.Adoption || result.ServicesReady {
		return errors.New("MANAGED_RESOURCE_PREPARATION_REQUIRES_READBACK")
	}
	return nil
}

func transferControllerCredential(process Process, member int64, token []byte) error {
	output, err := runInstalledSharedAdmin(process, nil, "managed-credentials", "status")
	var observed struct {
		Status   string `json:"status"`
		MemberID int64  `json:"memberId"`
		Code     string `json:"code"`
	}
	if err != nil || json.Unmarshal(output, &observed) != nil {
		return errors.New("MANAGED_CREDENTIAL_STATUS_REQUIRES_READBACK")
	}
	var operation string
	switch observed.Status {
	case "manual", "disconnected":
		operation = "intake"
	case "active":
		if observed.MemberID == member {
			operation = "replace"
		}
	}
	if operation == "" {
		return errors.New("MANAGED_CREDENTIAL_STATUS_REQUIRES_READBACK")
	}
	payload, err := codexCredentialEnvelope(token)
	if err != nil {
		return err
	}
	defer clear(payload)
	output, err = runInstalledSharedAdmin(process, payload, "managed-credentials",
		operation)
	observed = struct {
		Status   string `json:"status"`
		MemberID int64  `json:"memberId"`
		Code     string `json:"code"`
	}{}
	if err != nil || json.Unmarshal(output, &observed) != nil ||
		observed.Status != "active" || observed.MemberID != member {
		if observed.Code == "MANAGED_CODEX_DEDICATED_LOGIN_REQUIRED" {
			return errors.New(observed.Code)
		}
		return errors.New("MANAGED_CREDENTIAL_TRANSFER_REQUIRES_READBACK")
	}
	return nil
}

type installResultEncoder interface{ Encode(any) error }

func (a App) finishManagedInit(ctx context.Context, plan InitPlan, token []byte,
	encoder installResultEncoder) error {
	if err := requireInactiveSharedWake(a.API, plan.Repository.FullName); err != nil {
		return err
	}
	host, err := discoverSharedHost(a.Process)
	if err != nil {
		return err
	}
	if host == nil && plan.Repository.ID != plan.Execution.RepositoryID {
		return errors.New("MANAGED_INSTALL_EXECUTION_HOST_REQUIRED")
	}
	if host != nil && (host.RepositoryID != plan.Execution.RepositoryID ||
		host.Repository != plan.Execution.Repository || host.MemberID != plan.Member.ID) {
		return errors.New("MANAGED_INSTALL_HOST_BINDING_CHANGED")
	}
	var packages []string
	if host == nil {
		if _, err := prepareSharedNode(a.Process); err != nil {
			return err
		}
		if _, err := deploySharedCore(a.API, a.Process); err != nil {
			return err
		}
		if err := prepareManagedHost(a.Process, plan); err != nil {
			return err
		}
		if err := transferControllerCredential(a.Process, plan.Member.ID, token); err != nil {
			return err
		}
		packages, err = a.stageSharedRunnerPackages(plan)
		if err != nil {
			return err
		}
		runners, err := a.registerInitSharedSlots(plan, packages)
		if err != nil {
			return err
		}
		host, err = adoptInitialSharedHost(a.Process, plan, runners)
		if err != nil || host == nil {
			return errors.New("MANAGED_HOST_ADOPTION_PENDING")
		}
	} else if plan.Repository.ID != host.RepositoryID &&
		host.adoptionStatus(plan.Repository.ID) != "observed" {
		if err := adoptSharedPeer(a.Process, *host, plan); err != nil {
			return err
		}
		host, err = discoverSharedHost(a.Process)
		if err != nil || host == nil || host.adoptionStatus(plan.Repository.ID) != "observed" {
			return errors.New("MANAGED_HOST_ADOPTION_PENDING")
		}
	}
	if plan.Repository.ID == host.RepositoryID && len(packages) == 0 {
		if live, readErr := readStartedSharedRunners(ctx, a.API, *host); readErr == nil {
			return publishManagedHandoff(encoder, plan, live)
		}
		// Adoption may have completed before the first service start.
		staged := make([]sharedRegistration, 0, 2)
		for _, slot := range host.Runners {
			staged = append(staged, sharedRegistration{ID: slot.ID, Name: slot.Name,
				Status: "offline"})
		}
		packages, err = a.readStagedSharedSlots(plan, staged)
		if err != nil {
			return err
		}
	}
	if len(packages) != 0 {
		if _, err := installSharedService(a.Process, packages, true); err != nil {
			return err
		}
		if err := retireStagedRegistration(packages); err != nil {
			return err
		}
	}
	runners, err := readStartedSharedRunners(ctx, a.API, *host)
	if err != nil {
		return err
	}
	return publishManagedHandoff(encoder, plan, runners)
}

func publishManagedHandoff(encoder installResultEncoder, plan InitPlan,
	runners []sharedRegistration) error {
	return encoder.Encode(struct {
		Status       string               `json:"status"`
		Code         string               `json:"code"`
		Wake         string               `json:"wake"`
		MemberID     int64                `json:"member_id"`
		RepositoryID int64                `json:"repository_id"`
		Execution    *SharedExecution     `json:"execution"`
		Source       Producers            `json:"source"`
		Runners      []sharedRegistration `json:"runners"`
	}{"completed", "MANAGED_INSTALLATION_INACTIVE", "inactive",
		plan.Member.ID, plan.Repository.ID, plan.Execution, Pins(), runners})
}
