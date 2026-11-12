package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	_ "embed"
)

//go:embed producers.json
var producerJSON []byte

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type SourcePin struct {
	Repository   string `json:"repository"`
	Commit       string `json:"commit"`
	ArchiveHash  string `json:"archiveHash,omitempty"`
	ManifestHash string `json:"manifestHash,omitempty"`
}

type Producers struct {
	Core     SourcePin `json:"core"`
	Action   SourcePin `json:"action"`
	Artifact struct {
		ID     int64  `json:"id"`
		Digest string `json:"digest"`
	} `json:"artifact"`
}

func Pins() Producers {
	var pins Producers
	if json.Unmarshal(producerJSON, &pins) != nil {
		panic("invalid embedded producer pins")
	}
	return pins
}

type Content struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
	SHA      string `json:"sha"`
}

func blobHash(data []byte) string {
	return gitObjectHash("blob", data)
}

func content(api API, repo, path, ref string) ([]byte, string, error) {
	var file Content
	endpoint := "repos/" + repo + "/contents/" + path + "?ref=" + url.QueryEscape(ref)
	if err := api.Call("GET", endpoint, nil, &file); err != nil {
		return nil, "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil || file.Type != "file" || file.Encoding != "base64" ||
		len(data) > 1024*1024 || blobHash(data) != file.SHA {
		return nil, "", errors.New("SOURCE_CONTENT_MISMATCH")
	}
	return data, file.SHA, nil
}

func verifyProducers(api API, pins Producers) error {
	if !digestPattern.MatchString(pins.Core.ArchiveHash) ||
		!digestPattern.MatchString(pins.Core.ManifestHash) || pins.Artifact.ID <= 0 ||
		!strings.HasPrefix(pins.Artifact.Digest, "sha256:") ||
		!digestPattern.MatchString(strings.TrimPrefix(pins.Artifact.Digest, "sha256:")) {
		return errors.New("INVALID_PRODUCER_PIN")
	}
	for _, pin := range []SourcePin{pins.Core, pins.Action} {
		if !repoPattern.MatchString(pin.Repository) || !commitPattern.MatchString(pin.Commit) {
			return errors.New("INVALID_PRODUCER_PIN")
		}
		var repo Repository
		if err := api.Call("GET", "repos/"+pin.Repository, nil, &repo); err != nil {
			return err
		}
		if repo.Private || repo.FullName != pin.Repository {
			return errors.New("PUBLIC_PRODUCER_REQUIRED")
		}
		var commit struct {
			SHA string `json:"sha"`
		}
		path := "repos/" + pin.Repository + "/commits/" + pin.Commit
		if err := api.Call("GET", path, nil, &commit); err != nil {
			return err
		}
		if commit.SHA != pin.Commit {
			return errors.New("PRODUCER_COMMIT_MISMATCH")
		}
	}
	data, _, err := content(api, pins.Action.Repository, "core-source.json",
		pins.Action.Commit)
	if err != nil {
		return err
	}
	var bound SourcePin
	if json.Unmarshal(data, &bound) != nil || bound != pins.Core {
		return errors.New("INCOMPATIBLE_PRODUCER_PINS")
	}
	return nil
}
