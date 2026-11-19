package cli

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
)

type nativeSourceTarget struct {
	RunID      int64 `json:"runId"`
	ArtifactID int64 `json:"artifactId"`
}

type nativeSourceArtifact struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Expired bool   `json:"expired"`
	Run     struct {
		ID int64 `json:"id"`
	} `json:"workflow_run"`
}

func selectNativeOperationTarget(api API, pins Producers,
	receipt maintenanceReceipt) (nativeSourceTarget, error) {
	if maintenanceOperationMatches(receipt, pins) {
		return *receipt.OperationTarget, nil
	}
	return findNativeSourceTarget(api, pins)
}

// Locate the already published native operation for these verified sources.
// This explicit apply read neither dispatches a workflow nor guesses "latest".
func findNativeSourceTarget(api API, pins Producers) (nativeSourceTarget, error) {
	var empty nativeSourceTarget
	name := "nibfin-activate-" + pins.Core.Commit
	base := "repos/" + officialCore
	for page := 1; ; page++ {
		var listing struct {
			Total     *int                   `json:"total_count"`
			Artifacts []nativeSourceArtifact `json:"artifacts"`
		}
		path := fmt.Sprintf("%s/actions/artifacts?name=%s&per_page=100&page=%d",
			base, url.QueryEscape(name), page)
		if err := api.Call("GET", path, nil, &listing); err != nil {
			return empty, err
		}
		if listing.Total == nil || *listing.Total < 0 || listing.Artifacts == nil {
			return empty, errors.New("CORE_SOURCE_ACTIVATION_LIST_INVALID")
		}
		for _, artifact := range listing.Artifacts {
			if artifact.Name != name || artifact.Expired || artifact.ID <= 0 ||
				artifact.Run.ID <= 0 {
				continue
			}
			if err := verifyNativeSourceRun(api, artifact.Run.ID); err != nil {
				return empty, err
			}
			var data []byte
			path := fmt.Sprintf("%s/actions/artifacts/%d/zip", base, artifact.ID)
			if err := api.Call("GET", path, nil, &data); err != nil {
				return empty, err
			}
			selected, err := nativeActivationPins(data)
			if err != nil {
				return empty, err
			}
			if err := verifyProducers(api, selected); err != nil {
				return empty, err
			}
			if sameProducerSources(selected, pins) {
				return nativeSourceTarget{artifact.Run.ID, artifact.ID}, nil
			}
		}
		// GitHub's documented REST maximum page size; no local history cap.
		if len(listing.Artifacts) < 100 {
			return empty, &CommandError{Code: "CORE_SOURCE_ACTIVATION_NOT_PUBLISHED", Exit: 2}
		}
	}
}

func nativeActivationPins(data []byte) (Producers, error) {
	var pins Producers
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) != 1 || archive.File[0].Name != "activation.json" {
		return pins, errors.New("CORE_SOURCE_ACTIVATION_INVALID")
	}
	stream, err := archive.File[0].Open()
	if err != nil {
		return pins, err
	}
	defer stream.Close()
	content, err := io.ReadAll(stream)
	if err != nil || decodeUpgradePins(content, &pins) != nil {
		return pins, errors.New("CORE_SOURCE_ACTIVATION_INVALID")
	}
	return pins, nil
}

func verifyNativeSourceRun(api API, id int64) error {
	base := "repos/" + officialCore
	var repo struct {
		Repository
		Archived bool `json:"archived"`
		Disabled bool `json:"disabled"`
	}
	var run struct {
		ID         int64      `json:"id"`
		Repository Repository `json:"repository"`
		HeadRepo   Repository `json:"head_repository"`
		WorkflowID int64      `json:"workflow_id"`
		Path       string     `json:"path"`
		HeadBranch string     `json:"head_branch"`
		HeadSHA    string     `json:"head_sha"`
		Event      string     `json:"event"`
		Status     string     `json:"status"`
		Conclusion string     `json:"conclusion"`
	}
	var workflow struct {
		ID    int64  `json:"id"`
		Path  string `json:"path"`
		State string `json:"state"`
	}
	if err := api.Call("GET", base, nil, &repo); err != nil {
		return err
	}
	if err := api.Call("GET", fmt.Sprintf("%s/actions/runs/%d", base, id),
		nil, &run); err != nil {
		return err
	}
	if err := api.Call("GET", base+"/actions/workflows/cd.yml", nil, &workflow); err != nil {
		return err
	}
	if repo.FullName != officialCore || repo.ID <= 0 || repo.Private ||
		repo.Archived || repo.Disabled ||
		repo.DefaultBranch != "main" || run.ID != id || run.Repository.ID != repo.ID ||
		run.HeadRepo.ID != repo.ID || run.Path != ".github/workflows/cd.yml" ||
		run.HeadBranch != repo.DefaultBranch || !commitPattern.MatchString(run.HeadSHA) ||
		(run.Event != "workflow_run" && run.Event != "workflow_dispatch") ||
		run.Status != "completed" || run.Conclusion != "success" ||
		workflow.ID <= 0 || run.WorkflowID != workflow.ID ||
		workflow.Path != ".github/workflows/cd.yml" || workflow.State != "active" {
		return errors.New("CORE_SOURCE_ACTIVATION_RUN_UNTRUSTED")
	}
	return nil
}
