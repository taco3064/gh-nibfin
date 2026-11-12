package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
)

const officialCLI = "taco3064/gh-nibfin"

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.` +
	`(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type upgradeSelection struct {
	Version   string `json:"version,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

type upgradeSource struct {
	CLICommit string    `json:"cli_commit"`
	Evidence  string    `json:"evidence"`
	Pins      Producers `json:"pins"`
}

func decodeProducers(data []byte) (Producers, error) {
	var pins Producers
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pins) != nil || decoder.Decode(new(any)) != io.EOF {
		return pins, errors.New("INVALID_PRODUCER_METADATA")
	}
	// Only the official producer pair is supported, including installed sources.
	official := Pins()
	if pins.Core.Repository != official.Core.Repository ||
		pins.Action.Repository != official.Action.Repository {
		return pins, errors.New("OFFICIAL_PRODUCERS_REQUIRED")
	}
	return pins, nil
}

func resolveUpgrade(api API, selection upgradeSelection) (upgradeSource, error) {
	var source upgradeSource
	ref := selection.Candidate
	source.Evidence = "exact_commit_candidate_package_not_release_installation"
	if selection.Version != "" {
		if ref != "" || !versionPattern.MatchString(selection.Version) {
			return source, errors.New("INVALID_UPGRADE_SOURCE")
		}
		var release struct {
			ID         int64  `json:"id"`
			Tag        string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
			URL        string `json:"html_url"`
		}
		path := "repos/" + officialCLI + "/releases/tags/" + selection.Version
		if err := api.Call("GET", path, nil, &release); err != nil {
			return source, err
		}
		want := "https://github.com/" + officialCLI + "/releases/tag/" + selection.Version
		if release.ID <= 0 || release.Tag != selection.Version || release.Draft ||
			release.Prerelease || release.URL != want {
			return source, errors.New("OFFICIAL_VERSION_UNAVAILABLE")
		}
		var err error
		ref, err = officialVersionCommit(api, selection.Version)
		if err != nil {
			return source, err
		}
		source.Evidence = "official_release_metadata_with_verified_candidate_package"
	} else if !commitPattern.MatchString(ref) {
		return source, errors.New("INVALID_UPGRADE_SOURCE")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	path := "repos/" + officialCLI + "/commits/" + url.PathEscape(ref)
	if err := api.Call("GET", path, nil, &commit); err != nil {
		return source, err
	}
	if !commitPattern.MatchString(commit.SHA) || commit.SHA != ref {
		return source, errors.New("UPGRADE_SOURCE_COMMIT_MISMATCH")
	}
	source.CLICommit = commit.SHA
	data, _, err := content(api, officialCLI, "internal/cli/producers.json", commit.SHA)
	if err != nil {
		return source, err
	}
	source.Pins, err = decodeProducers(data)
	if err != nil {
		return source, err
	}
	if err := verifyProducers(api, source.Pins); err != nil {
		return source, err
	}
	// Preview verifies package identities and digests, but never extracts or installs.
	if _, err := candidateArchive(api, source.Pins); err != nil {
		return source, err
	}
	return source, nil
}

func officialVersionCommit(api API, version string) (string, error) {
	type object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object object `json:"object"`
	}
	base := "repos/" + officialCLI
	if err := api.Call("GET", base+"/git/ref/tags/"+version, nil, &ref); err != nil {
		return "", err
	}
	if ref.Ref != "refs/tags/"+version {
		return "", errors.New("OFFICIAL_VERSION_TAG_MISMATCH")
	}
	current := ref.Object
	for depth := 0; depth < 8; depth++ {
		if !commitPattern.MatchString(current.SHA) {
			break
		}
		if current.Type == "commit" {
			return current.SHA, nil
		}
		if current.Type != "tag" {
			break
		}
		var tag struct {
			SHA    string `json:"sha"`
			Object object `json:"object"`
		}
		if err := api.Call("GET", base+"/git/tags/"+current.SHA, nil, &tag); err != nil {
			return "", err
		}
		if tag.SHA != current.SHA {
			break
		}
		current = tag.Object
	}
	return "", errors.New("OFFICIAL_VERSION_TAG_UNVERIFIABLE")
}
