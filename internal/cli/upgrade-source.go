package cli

import (
	"errors"
	"net/url"
	"regexp"
)

const officialCLI = "taco3064/gh-nibfin"
const officialCore = "taco3064/nibfin-core"
const officialAction = "taco3064/nibfin-act"

var versionPattern = regexp.MustCompile(
	`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type upgradeSource struct {
	Version   string    `json:"version"`
	CLI       SourcePin `json:"cli"`
	Producers Producers `json:"producers"`
	recipes   map[string][]string
}

// A release is a locator. Only the peeled, read-back official commit supplies pins.
func resolveUpgrade(api API, version string) (upgradeSource, error) {
	result := upgradeSource{}
	if version != "latest" && !versionPattern.MatchString(version) {
		return result, errors.New("UNSUPPORTED_UPGRADE_VERSION")
	}
	var repo Repository
	if err := api.Call("GET", "repos/"+officialCLI, nil, &repo); err != nil {
		return result, err
	}
	if repo.FullName != officialCLI || repo.Private || repo.ID <= 0 {
		return result, errors.New("OFFICIAL_CLI_REQUIRED")
	}
	endpoint := "repos/" + officialCLI + "/releases/latest"
	if version != "latest" {
		endpoint = "repos/" + officialCLI + "/releases/tags/" + url.PathEscape(version)
	}
	var release struct {
		ID         int64  `json:"id"`
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := api.Call("GET", endpoint, nil, &release); err != nil {
		return result, err
	}
	if release.ID <= 0 || release.Draft || release.Prerelease ||
		!versionPattern.MatchString(release.Tag) ||
		version != "latest" && version != release.Tag ||
		release.URL != "https://github.com/"+officialCLI+"/releases/tag/"+release.Tag {
		return result, errors.New("UNSUPPORTED_OFFICIAL_RELEASE_METADATA")
	}
	var tag struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := api.Call("GET", "repos/"+officialCLI+"/git/ref/tags/"+
		url.PathEscape(release.Tag), nil, &tag); err != nil {
		return result, err
	}
	if tag.Ref != "refs/tags/"+release.Tag || !commitPattern.MatchString(tag.Object.SHA) {
		return result, errors.New("UNRESOLVED_OFFICIAL_VERSION")
	}
	// Bound annotated-tag peeling; cycles and unsupported object types fail closed.
	for depth := 0; tag.Object.Type == "tag" && depth < 8; depth++ {
		var annotated struct {
			SHA    string `json:"sha"`
			Object struct {
				SHA  string `json:"sha"`
				Type string `json:"type"`
			} `json:"object"`
		}
		if err := api.Call("GET", "repos/"+officialCLI+"/git/tags/"+
			tag.Object.SHA, nil, &annotated); err != nil {
			return result, err
		}
		if annotated.SHA != tag.Object.SHA ||
			!commitPattern.MatchString(annotated.Object.SHA) {
			return result, errors.New("UNRESOLVED_OFFICIAL_VERSION")
		}
		tag.Object = annotated.Object
	}
	if tag.Object.Type != "commit" {
		return result, errors.New("UNRESOLVED_OFFICIAL_VERSION")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := api.Call("GET", "repos/"+officialCLI+"/commits/"+
		tag.Object.SHA, nil, &commit); err != nil {
		return result, err
	}
	if commit.SHA != tag.Object.SHA {
		return result, errors.New("OFFICIAL_CLI_COMMIT_MISMATCH")
	}
	data, _, err := content(api, officialCLI, "internal/cli/producers.json", commit.SHA)
	if err != nil {
		return result, err
	}
	if err := decodeUpgradePins(data, &result.Producers); err != nil {
		return result, err
	}
	result.recipes, err = upgradeRecipes(api, result.Producers)
	result.Version = release.Tag
	result.CLI = SourcePin{Repository: officialCLI, Commit: commit.SHA}
	return result, err
}

func upgradeRecipes(api API, pins Producers) (map[string][]string, error) {
	if pins.Core.Repository != officialCore || pins.Action.Repository != officialAction ||
		pins.Action.ArchiveHash != "" || pins.Action.ManifestHash != "" {
		return nil, errors.New("OFFICIAL_PRODUCERS_REQUIRED")
	}
	if err := verifyProducers(api, pins); err != nil {
		return nil, err
	}
	archive, err := candidateArchive(api, pins)
	if err != nil {
		return nil, err
	}
	files, err := readCoreArchive(archive)
	if err != nil {
		return nil, err
	}
	manifestFound := false
	for _, file := range files {
		if sha256Hash(file.data) == pins.Core.ManifestHash {
			manifestFound = true
		}
	}
	if !manifestFound {
		return nil, errors.New("CORE_MANIFEST_HASH_MISMATCH")
	}
	data, _, err := content(api, pins.Core.Repository,
		"src/caller-recipes.json", pins.Core.Commit)
	if err != nil {
		return nil, err
	}
	if string(files["src/caller-recipes.json"].data) != string(data) {
		return nil, errors.New("CORE_ARCHIVE_RECIPE_MISMATCH")
	}
	return decodeCallerRecipes(data, pins.Core)
}
