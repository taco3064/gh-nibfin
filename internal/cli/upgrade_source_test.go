package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func setFixtureTarget(f *upgradeAPI, pins Producers) {
	data, _ := json.Marshal(pins)
	f.data["repos/"+officialCLI+"/contents/internal/cli/producers.json?ref="+
		strings.Repeat("5", 40)] = encoded(data)
}

func TestUpgradeVersionAndSourceRejections(t *testing.T) {
	kinds := []string{"unavailable-release", "draft", "prerelease", "unsupported-tag",
		"foreign-release-url", "wrong-cli-repo", "private-cli", "floating-tag",
		"wrong-tag", "tag-tree", "missing-tag", "missing-cli", "wrong-cli-commit",
		"malformed-pins", "foreign-core", "foreign-action", "floating-core",
		"wrong-source-commit", "binding", "artifact-digest", "archive-pin",
		"archive-digest", "manifest-digest", "missing-recipes", "bad-recipe-blob"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			releasePath := "repos/" + officialCLI + "/releases/latest"
			release := f.data[releasePath].(map[string]any)
			tagPath := "repos/" + officialCLI + "/git/ref/tags/v1.2.3"
			tag := f.data[tagPath].(map[string]any)
			object := tag["object"].(map[string]string)
			cliPath := "repos/" + officialCLI + "/commits/" + strings.Repeat("5", 40)
			pinsPath := "repos/" + officialCLI + "/contents/internal/cli/producers.json?ref=" +
				strings.Repeat("5", 40)
			pins := f.target
			artifactPath := fmt.Sprintf("repos/%s/actions/artifacts/%d",
				officialCore, pins.Artifact.ID)
			switch kind {
			case "unavailable-release":
				delete(f.data, releasePath)
			case "draft", "prerelease":
				release[kind] = true
			case "unsupported-tag":
				release["tag_name"] = "main"
			case "foreign-release-url":
				release["html_url"] = "https://github.com/evil/repo"
			case "wrong-cli-repo", "private-cli":
				repo := f.data["repos/"+officialCLI].(Repository)
				if kind == "wrong-cli-repo" {
					repo.FullName = "evil/gh-nibfin"
				} else {
					repo.Private = true
				}
				f.data["repos/"+officialCLI] = repo
			case "floating-tag":
				object["sha"] = "main"
			case "wrong-tag":
				tag["ref"] = "refs/tags/v9.9.9"
			case "tag-tree":
				object["type"] = "tree"
			case "missing-tag":
				delete(f.data, tagPath)
			case "missing-cli":
				delete(f.data, cliPath)
			case "wrong-cli-commit":
				f.data[cliPath] = map[string]string{"sha": "main"}
			case "malformed-pins":
				f.data[pinsPath] = encoded([]byte("not JSON"))
			case "foreign-core":
				pins.Core.Repository = "evil/nibfin-core"
				setFixtureTarget(f, pins)
			case "foreign-action":
				pins.Action.Repository = "evil/nibfin-act"
				setFixtureTarget(f, pins)
			case "floating-core":
				pins.Core.Commit = "main"
				setFixtureTarget(f, pins)
			case "wrong-source-commit":
				f.data["repos/"+officialCore+"/commits/"+pins.Core.Commit] =
					map[string]string{"sha": strings.Repeat("9", 40)}
			case "binding":
				f.data["repos/"+officialAction+"/contents/core-source.json?ref="+
					pins.Action.Commit] = encoded([]byte(`{}`))
			case "artifact-digest":
				f.data[artifactPath] = map[string]any{"id": pins.Artifact.ID, "digest": "sha256:bad"}
			case "archive-pin", "manifest-digest":
				if kind == "archive-pin" {
					pins.Core.ArchiveHash = strings.Repeat("0", 64)
				} else {
					pins.Core.ManifestHash = strings.Repeat("0", 64)
				}
				bound, _ := json.Marshal(pins.Core)
				f.data["repos/"+officialAction+"/contents/core-source.json?ref="+
					pins.Action.Commit] = encoded(bound)
				setFixtureTarget(f, pins)
			case "archive-digest":
				f.data[artifactPath+"/zip"] = []byte("tampered zip")
			case "missing-recipes":
				delete(f.data, "repos/"+officialCore+"/contents/src/caller-recipes.json?ref="+
					pins.Core.Commit)
			case "bad-recipe-blob":
				file := encoded([]byte("{}"))
				file.SHA = strings.Repeat("e", 40)
				f.data["repos/"+officialCore+"/contents/src/caller-recipes.json?ref="+
					pins.Core.Commit] = file
			}
			if _, err := resolveUpgrade(f, "latest"); err == nil {
				t.Fatal("accepted", kind)
			}
			if len(f.writes) != 0 {
				t.Fatal(f.writes)
			}
		})
	}
}

func TestUpgradeAnnotatedTagsAndVersionMismatch(t *testing.T) {
	f := upgradeFixture(t, false, true)
	tagSHA := strings.Repeat("6", 40)
	f.data["repos/"+officialCLI+"/git/ref/tags/v1.2.3"] = map[string]any{
		"ref": "refs/tags/v1.2.3", "object": map[string]string{"type": "tag", "sha": tagSHA}}
	tag := map[string]any{"sha": tagSHA, "object": map[string]string{
		"type": "commit", "sha": strings.Repeat("5", 40)}}
	f.data["repos/"+officialCLI+"/git/tags/"+tagSHA] = tag
	source, err := resolveUpgrade(f, "v1.2.3")
	if err != nil || source.CLI.Commit != strings.Repeat("5", 40) {
		t.Fatal(source, err)
	}
	tag["object"] = map[string]string{"type": "tag", "sha": tagSHA}
	if _, err := resolveUpgrade(f, "latest"); err == nil {
		t.Fatal("cycle")
	}
	for _, version := range []string{"main", "v01.2.3", "v1.2", "v1.2.3-rc1", ""} {
		if _, err := resolveUpgrade(f, version); err == nil {
			t.Fatal(version)
		}
	}
	release := f.data["repos/"+officialCLI+"/releases/tags/v1.2.3"].(map[string]any)
	release["tag_name"] = "v9.9.9"
	if _, err := resolveUpgrade(f, "v1.2.3"); err == nil {
		t.Fatal("version mismatch")
	}
}

func TestUpgradeArchiveManifestAndRecipeBytesAreBound(t *testing.T) {
	for _, kind := range []string{"manifest", "recipes"} {
		f := upgradeFixture(t, false, true)
		pins := f.target
		archive, err := candidateArchive(f, pins)
		if err != nil {
			t.Fatal(err)
		}
		original, err := readCoreArchive(archive)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{}
		for name, file := range original {
			files[name] = file.data
		}
		want := "CORE_MANIFEST_HASH_MISMATCH"
		if kind == "manifest" {
			files["manifest.json"] = []byte("altered manifest")
		} else {
			files["src/caller-recipes.json"] = []byte("altered recipes")
			want = "CORE_ARCHIVE_RECIPE_MISMATCH"
		}
		fixtureArchive(t, f.fakeAPI, &pins, files)
		bound, _ := json.Marshal(pins.Core)
		f.data["repos/"+officialAction+"/contents/core-source.json?ref="+
			pins.Action.Commit] = encoded(bound)
		setFixtureTarget(f, pins)
		if _, err := resolveUpgrade(f, "latest"); err == nil || err.Error() != want {
			t.Fatal(kind, err)
		}
	}
}

func TestProducerBindingUsesExactActionJSONFieldNames(t *testing.T) {
	for _, kind := range []string{"alias", "extra", "foreign-effective-field"} {
		f := upgradeFixture(t, false, true)
		pins := f.target
		bound, _ := json.Marshal(pins.Core)
		text := string(bound)
		switch kind {
		case "alias":
			text = strings.Replace(text, `"repository"`, `"Repository"`, 1)
		case "extra":
			text = strings.TrimSuffix(text, "}") + `,"newBinding": "unknown"}`
		case "foreign-effective-field":
			text = strings.Replace(text, `"repository":"`+officialCore+`"`,
				`"repository":"evil/core","Repository":"`+officialCore+`"`, 1)
		}
		f.data["repos/"+officialAction+"/contents/core-source.json?ref="+
			pins.Action.Commit] = encoded([]byte(text))
		if err := verifyProducers(f, pins); err == nil {
			t.Fatal(kind)
		}
	}
}
