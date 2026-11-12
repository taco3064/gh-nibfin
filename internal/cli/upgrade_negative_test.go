package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeRefusesUnrecognizedInstallationsAndDeniedRepositories(t *testing.T) {
	for _, problem := range []string{"missing", "malformed", "unofficial", "edit",
		"member", "unverified", "private", "permission", "unknown-layout"} {
		f, selection, _ := upgradeFixture(t)
		head := strings.Repeat("a", 40)
		base := upgradeRepo + "/contents/"
		repo := f.data[upgradeRepo].(Repository)
		switch problem {
		case "missing":
			delete(f.data, base+producerPath+"?ref="+head)
		case "malformed":
			f.data[base+producerPath+"?ref="+head] = encoded([]byte("{} garbage"))
		case "unofficial":
			pins := Pins()
			pins.Core.Repository = "other/core"
			putJSON(f.fakeAPI, base+producerPath+"?ref="+head, pins)
		case "edit", "member":
			f.data[base+".github/workflows/nibfin-wake.yml?ref="+head] =
				encoded([]byte("unrecognized user workflow"))
		case "unverified":
			f.data["repos/"+Pins().Core.Repository+"/commits/"+strings.Repeat("1", 40)] =
				map[string]string{"sha": strings.Repeat("f", 40)}
		case "private":
			repo.Private = true
		case "permission":
			repo.Permissions["push"] = false
		case "unknown-layout":
			putJSON(f.fakeAPI, "repos/"+Pins().Core.Repository+
				"/contents/src/caller-recipes.json?ref="+strings.Repeat("1", 40),
				map[string][]string{"unknown": {"recipe", ""}})
		}
		f.data[upgradeRepo] = repo
		if _, err := planUpgrade(f, "example/project", selection); err == nil ||
			len(f.writes) != 0 {
			t.Fatalf("%s must fail without mutations: %v", problem, err)
		}
	}
}

func TestUpgradeProductionSourceLookupFailsClosed(t *testing.T) {
	for _, problem := range []string{"missing-release", "draft", "wrong-repo",
		"prerelease", "wrong-tag", "wrong-commit", "bad-tag", "tag-cycle",
		"malformed", "expired", "digest", "archive", "incompatible", "content"} {
		f, selection, target := upgradeFixture(t)
		base := "repos/" + officialCLI
		releasePath := base + "/releases/tags/v1.2.3"
		release := f.data[releasePath].(map[string]any)
		artifact := fmt.Sprintf("repos/%s/actions/artifacts/%d",
			target.Core.Repository, target.Artifact.ID)
		switch problem {
		case "missing-release":
			delete(f.data, releasePath)
		case "draft":
			release["draft"] = true
		case "wrong-repo":
			release["html_url"] = "https://github.com/other/repo/releases/tag/v1.2.3"
		case "prerelease":
			release["prerelease"] = true
		case "wrong-tag":
			release["tag_name"] = "v0.0.1"
		case "wrong-commit":
			f.data[base+"/commits/"+strings.Repeat("5", 40)] =
				map[string]string{"sha": strings.Repeat("6", 40)}
		case "bad-tag", "tag-cycle":
			sha := strings.Repeat("5", 40)
			f.data[base+"/git/ref/tags/v1.2.3"] = map[string]any{
				"ref": "refs/tags/v1.2.3", "object": map[string]string{
					"type": "tag", "sha": sha}}
			f.data[base+"/git/tags/"+sha] = map[string]any{
				"sha": sha, "object": map[string]string{"type": "tag", "sha": sha}}
			if problem == "bad-tag" {
				delete(f.data, base+"/git/tags/"+sha)
			}
		case "malformed":
			f.data[base+"/contents/internal/cli/producers.json?ref="+
				strings.Repeat("5", 40)] = encoded([]byte(`{"unknown":true}`))
		case "expired":
			f.data[artifact].(map[string]any)["expired"] = true
		case "digest":
			f.data[artifact].(map[string]any)["digest"] = "sha256:" + strings.Repeat("0", 64)
		case "archive":
			f.data[artifact+"/zip"] = []byte("corrupt archive")
		case "incompatible":
			putJSON(f.fakeAPI, "repos/"+target.Action.Repository+
				"/contents/core-source.json?ref="+target.Action.Commit, Pins().Core)
		case "content":
			endpoint := base + "/contents/internal/cli/producers.json?ref=" +
				strings.Repeat("5", 40)
			file := f.data[endpoint].(Content)
			file.SHA = strings.Repeat("0", 40)
			f.data[endpoint] = file
		}
		if _, err := planUpgrade(f, "example/project", selection); err == nil ||
			len(f.writes) != 0 {
			t.Fatalf("%s must fail before mutations: %v", problem, err)
		}
	}
}

func TestUpgradeRevalidatesExactPlanBeforeCacheOrProviderMutation(t *testing.T) {
	for _, problem := range []string{"base", "content", "permission", "target", "private"} {
		f, selection, _ := upgradeFixture(t)
		plan, err := planUpgrade(f, "example/project", selection)
		if err != nil {
			t.Fatal(err)
		}
		switch problem {
		case "base":
			f.data[upgradeRepo+"/git/ref/heads/main"] = map[string]any{
				"object": map[string]string{"sha": strings.Repeat("b", 40)}}
		case "content":
			f.data[upgradeRepo+"/contents/.github/workflows/nibfin-issue.yml?ref="+
				plan.Head] = encoded([]byte("edited"))
		case "permission", "private":
			repo := f.data[upgradeRepo].(Repository)
			if problem == "private" {
				repo.Private = true
			} else {
				repo.Permissions["push"] = false
			}
			f.data[upgradeRepo] = repo
		case "target":
			f.data["repos/"+officialCLI+"/git/ref/tags/v1.2.3"] = map[string]any{
				"ref": "refs/tags/v1.2.3", "object": map[string]string{
					"type": "commit", "sha": strings.Repeat("6", 40)}}
		}
		result, err := (App{API: f, Process: forbiddenUpgradeProcess{t}}).applyUpgrade(plan)
		if err == nil || result.Status != "pending" || len(f.writes) != 0 {
			t.Fatalf("%s stale plan applied: %+v %v", problem, result, err)
		}
	}
}

func TestUpgradeRefusesBranchAndPRConflicts(t *testing.T) {
	for _, problem := range []string{"branch", "branch-race", "pr-head", "pr-base",
		"pr-repo", "closed", "duplicate", "revoke"} {
		f, selection, _ := upgradeFixture(t)
		isolatedUpgradeCache(t)
		plan, err := planUpgrade(f, "example/project", selection)
		if err != nil {
			t.Fatal(err)
		}
		app := App{API: f, Process: NativeProcess{}}
		if problem == "branch" {
			f.data[upgradeRefPath(plan)] = map[string]any{
				"object": map[string]string{"sha": strings.Repeat("f", 40)}}
		} else {
			if _, err := app.applyUpgrade(plan); err != nil {
				t.Fatal(err)
			}
			pull := f.pulls[0]
			switch problem {
			case "branch-race":
				f.hook = func(method, endpoint string) {
					if method == "GET" && strings.HasPrefix(endpoint, upgradeRepo+"/pulls?") {
						f.data[upgradeRefPath(plan)] = map[string]any{
							"object": map[string]string{"sha": strings.Repeat("f", 40)}}
					}
				}
			case "pr-head":
				pull.Head.SHA = strings.Repeat("f", 40)
			case "pr-base":
				pull.Base.SHA = strings.Repeat("f", 40)
			case "pr-repo":
				pull.Head.Repo.ID++
			case "closed":
				pull.State = "closed"
			case "duplicate":
				f.pulls = append(f.pulls, pull)
			case "revoke":
				f.hook = func(method, endpoint string) {
					if method == "GET" && strings.HasPrefix(endpoint, upgradeRepo+"/pulls?") {
						repo := f.data[upgradeRepo].(Repository)
						repo.Permissions["push"] = false
						f.data[upgradeRepo] = repo
					}
				}
			}
			f.data[upgradeRepo+"/pulls/9"] = pull
		}
		writes := len(f.writes)
		result, err := app.applyUpgrade(plan)
		if err == nil || result.Status != "pending" || len(f.writes) != writes {
			t.Fatalf("%s must not overwrite/retry: %+v %v", problem, result, err)
		}
	}
}

// Drive the public GitHub adapter as well as the resolver, including binary
// download responses. This remains controlled provider evidence, not live reads.
type upgradeTransport struct{ provider *upgradeProvider }

func (p upgradeTransport) Run(name string, args []string, data []byte) ([]byte, error) {
	if name != "gh" || len(args) < 8 || args[0] != "api" {
		return nil, errors.New("UNEXPECTED_PROVIDER_PROCESS")
	}
	method, endpoint := args[4], args[7]
	if method != "GET" || len(data) != 0 {
		return nil, errors.New("PREVIEW_PROVIDER_MUTATION")
	}
	if value, ok := p.provider.data[endpoint].([]byte); ok {
		return value, nil
	}
	var value any
	if err := p.provider.Call(method, endpoint, nil, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func TestUpgradeProductionAdapterAndCandidateSelection(t *testing.T) {
	f, _, target := upgradeFixture(t)
	api := GitHub{Process: upgradeTransport{f}}
	for _, selection := range []upgradeSelection{{Version: "v1.2.3"},
		{Candidate: strings.Repeat("5", 40)}} {
		source, err := resolveUpgrade(api, selection)
		if err != nil || source.Pins != target || source.CLICommit != strings.Repeat("5", 40) {
			t.Fatalf("production lookup: %+v %v", source, err)
		}
		if selection.Candidate != "" && !strings.Contains(source.Evidence, "not_release") {
			t.Fatal("candidate evidence must remain visibly distinct")
		}
	}
	// Annotated release tags are dereferenced by immutable tag-object identity.
	tag := strings.Repeat("8", 40)
	f.data["repos/"+officialCLI+"/git/ref/tags/v1.2.3"] = map[string]any{
		"ref": "refs/tags/v1.2.3", "object": map[string]string{"type": "tag", "sha": tag}}
	f.data["repos/"+officialCLI+"/git/tags/"+tag] = map[string]any{
		"sha": tag, "object": map[string]string{
			"type": "commit", "sha": strings.Repeat("5", 40)}}
	source, err := resolveUpgrade(api, upgradeSelection{Version: "v1.2.3"})
	if err != nil || source.Pins != target {
		t.Fatalf("annotated release tag: %+v %v", source, err)
	}
	var help bytes.Buffer
	if err := (App{}).Run([]string{"help"}, &help); err != nil ||
		!strings.Contains(help.String(), "upgrade") {
		t.Fatal("upgrade must be discoverable in help")
	}
}

func TestUpgradeRejectsModifiedCacheUsingArchiveVerifierBeforeRemoteWrites(t *testing.T) {
	f, selection, target := upgradeFixture(t)
	isolatedUpgradeCache(t)
	cache, err := installPinnedCoreCache(f, NativeProcess{}, target)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(cache, "bin", "nibfin-core.mjs")
	// If this cache file were run, it would falsely report success. The fresh
	// archive verifier must instead detect its manifest mismatch and reject it.
	malicious := "console.log(JSON.stringify({status:'verified',source:" +
		string(mustUpgradeJSON(t, target.Core)) + "}));\n"
	if err := os.WriteFile(entry, []byte(malicious), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := planUpgrade(f, "example/project", selection)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (App{API: f, Process: NativeProcess{}}).applyUpgrade(plan)
	if err == nil || err.Error() != "CORE_INSTALLATION_READBACK_FAILED" ||
		result.Status != "pending" || len(f.writes) != 0 {
		t.Fatalf("modified cache was trusted: %+v %v", result, err)
	}
	if data, err := os.ReadFile(entry); err != nil || string(data) != malicious {
		t.Fatal("existing cache data must remain available for inspection")
	}
}

func mustUpgradeJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
