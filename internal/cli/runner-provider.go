package cli

import (
	"errors"
	"fmt"
	"time"
)

type RunnerInfo struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func memberLabel(id int64) string { return fmt.Sprintf("nibfin-user-%d", id) }

func listRunners(api API, repo string) ([]RunnerInfo, error) {
	runners := []RunnerInfo{}
	for page := 1; page <= 100; page++ {
		var result struct {
			Total   *int         `json:"total_count"`
			Runners []RunnerInfo `json:"runners"`
		}
		path := fmt.Sprintf("repos/%s/actions/runners?per_page=100&page=%d", repo, page)
		if err := api.Call("GET", path, nil, &result); err != nil {
			return nil, err
		}
		if result.Total == nil || *result.Total < 0 || result.Runners == nil {
			return nil, errors.New("INVALID_RUNNER_LIST_RESULT")
		}
		runners = append(runners, result.Runners...)
		if len(runners) == *result.Total {
			return runners, nil
		}
		if len(result.Runners) == 0 || len(runners) > *result.Total {
			return nil, errors.New("RUNNER_LIST_CHANGED_OR_INCOMPLETE")
		}
	}
	return nil, errors.New("RUNNER_LIST_LIMIT")
}

func registrationToken(api API, repo, kind string, now time.Time) (string, error) {
	if kind != "registration-token" && kind != "remove-token" {
		return "", errors.New("INVALID_RUNNER_TOKEN_REQUEST")
	}
	var token struct {
		Value   string `json:"token"`
		Expires string `json:"expires_at"`
	}
	if err := api.Call("POST", "repos/"+repo+"/actions/runners/"+kind,
		nil, &token); err != nil {
		return "", err
	}
	expires, err := time.Parse(time.RFC3339, token.Expires)
	if err != nil || token.Value == "" || len(token.Value) > 16384 ||
		!expires.After(now.Add(30*time.Second)) {
		return "", errors.New("RUNNER_TOKEN_MISSING_OR_EXPIRED")
	}
	return token.Value, nil
}

func ownedRunner(api API, repo Repository, user User,
	record RunnerInstall) (RunnerInfo, error) {
	var runner RunnerInfo
	if record.RunnerID <= 0 {
		return runner, errors.New("RUNNER_REGISTRATION_RESULT_UNRESOLVED")
	}
	path := fmt.Sprintf("repos/%s/actions/runners/%d", repo.FullName, record.RunnerID)
	if err := api.Call("GET", path, nil, &runner); err != nil {
		return runner, err
	}
	if runner.ID != record.RunnerID || runner.Name != record.Name ||
		!hasMemberLabel(runner, user.ID) {
		return runner, errors.New("RUNNER_MANAGEMENT_IDENTITY_MISMATCH")
	}
	return runner, nil
}
