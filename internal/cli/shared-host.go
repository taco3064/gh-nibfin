package cli

import (
	"encoding/json"
	"errors"
	"slices"
)

type sharedHost struct {
	SharedExecution
	SchemaVersion        int     `json:"schemaVersion"`
	MemberID             int64   `json:"memberId"`
	AdoptedRepositoryIDs []int64 `json:"adoptedRepositoryIds"`
	Runners              []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runners"`
}

type sharedRegistration struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
}

func (host sharedHost) adoptionStatus(id int64) string {
	if host.AdoptedRepositoryIDs == nil {
		return "unverified"
	}
	if slices.Contains(host.AdoptedRepositoryIDs, id) {
		return "observed"
	}
	return "required"
}

func (host sharedHost) verifyInit(api API, executionRepo, runnerPath,
	runnerName string) ([]sharedRegistration, error) {
	if runnerPath != "" || runnerName != "" {
		return nil, errors.New("SHARED_EXECUTION_LEGACY_RUNNER_OPTIONS_CONFLICT")
	}
	if executionRepo != "" && executionRepo != host.Repository {
		return nil, errors.New("SHARED_EXECUTION_HOST_CONFLICT")
	}
	return verifySharedHost(api, host)
}

func recheckSharedHost(process Process, api API, planned sharedHost) error {
	host, err := discoverSharedHost(process)
	if err != nil {
		return err
	}
	if host == nil || host.SharedExecution != planned.SharedExecution ||
		host.MemberID != planned.MemberID || !slices.Equal(host.Runners, planned.Runners) ||
		!slices.Equal(host.AdoptedRepositoryIDs, planned.AdoptedRepositoryIDs) {
		return errors.New("SHARED_EXECUTION_HOST_CHANGED")
	}
	_, err = verifySharedHost(api, *host)
	return err
}

func decodeSharedHost(bytes []byte) (*sharedHost, error) {
	var host sharedHost
	if json.Unmarshal(bytes, &host) != nil || host.SchemaVersion != 1 ||
		host.RepositoryID <= 0 || !repoPattern.MatchString(host.Repository) ||
		host.MemberID <= 0 || len(host.Runners) != 2 {
		return nil, errors.New("INVALID_SHARED_HOST_INSPECTION")
	}
	first, second := host.Runners[0], host.Runners[1]
	if first.ID <= 0 || second.ID <= 0 || first.ID == second.ID ||
		first.Name == "" || second.Name == "" || first.Name == second.Name {
		return nil, errors.New("INVALID_SHARED_HOST_INSPECTION")
	}
	return &host, nil
}

func verifySharedHost(api API, host sharedHost) ([]sharedRegistration, error) {
	repo, user, err := repository(api, host.Repository, true)
	if err != nil {
		return nil, err
	}
	if repo.ID != host.RepositoryID || repo.FullName != host.Repository ||
		user.ID != host.MemberID {
		return nil, errors.New("SHARED_EXECUTION_HOST_CONFLICT")
	}
	runners, err := listRunners(api, repo.FullName)
	if err != nil {
		return nil, err
	}
	registrations := make([]sharedRegistration, 0, len(host.Runners))
	for _, binding := range host.Runners {
		matches := 0
		for _, runner := range runners {
			if runner.ID == binding.ID && runner.Name == binding.Name &&
				runner.OS == "Linux" && hasMemberLabel(runner, user.ID) {
				for _, label := range runner.Labels {
					if label.Name == "nibfin-shared" {
						matches++
						registrations = append(registrations, sharedRegistration{
							ID: runner.ID, Name: runner.Name,
							Status: runner.Status, Busy: runner.Busy,
						})
					}
				}
			}
		}
		if matches != 1 {
			return nil, errors.New("SHARED_RUNNER_REGISTRATION_CHANGED")
		}
	}
	return registrations, nil
}
