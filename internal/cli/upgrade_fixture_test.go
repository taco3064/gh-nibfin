package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
)

const upgradeRepo = "repos/example/project"

type upgradeAPI struct {
	*fakeAPI
	calls          []string
	old            Producers
	target         Producers
	installed      map[string]string
	baseEntries    map[string]upgradeEntry
	lost           string
	denyRead       string
	denyAfterWrite string
	beforeWrite    func(string)
	afterWrite     func(string)
}

func (f *upgradeAPI) Call(method, path string, input, output any) error {
	f.calls = append(f.calls, method+" "+path)
	if method == "GET" {
		if path == f.denyRead {
			return errors.New("READ_BACK_UNAVAILABLE")
		}
		return f.fakeAPI.Call(method, path, input, output)
	}
	f.writes = append(f.writes, method+" "+path)
	if f.beforeWrite != nil {
		f.beforeWrite(path)
	}
	if method != "POST" {
		return errors.New("UNEXPECTED_MUTATION")
	}
	switch path {
	case upgradeRepo + "/git/trees":
		body := input.(map[string]any)
		old := f.data[upgradeRepo+"/git/trees/"+
			body["base_tree"].(string)+"?recursive=1"].(upgradeTree)
		entries := map[string]upgradeEntry{}
		for _, entry := range old.Entries {
			entries[entry.Path] = entry
		}
		for _, item := range body["tree"].([]map[string]any) {
			name := item["path"].(string)
			text, exists := item["content"]
			if !exists {
				delete(entries, name)
				continue
			}
			entries[name] = upgradeEntry{Path: name, Mode: item["mode"].(string),
				Type: item["type"].(string), SHA: blobHash([]byte(text.(string)))}
			for parent := name; strings.Contains(parent, "/"); {
				parent = parent[:strings.LastIndex(parent, "/")]
				entries[parent] = upgradeEntry{Path: parent, Mode: "040000", Type: "tree"}
			}
		}
		fixtureTree(f.fakeAPI, entries)
	case upgradeRepo + "/git/commits":
		body := input.(map[string]any)
		identity := body["author"].(map[string]string)
		if identity["date"] != upgradeDate || body["committer"] == nil {
			return errors.New("COMMIT_IDENTITY_REQUIRED")
		}
		parent := body["parents"].([]string)[0]
		tree := body["tree"].(string)
		message := body["message"].(string)
		raw := fmt.Sprintf("tree %s\nparent %s\nauthor %s <%s> 946684800 +0000\n"+
			"committer %s <%s> 946684800 +0000\n\n%s", tree, parent,
			identity["name"], identity["email"], identity["name"], identity["email"], message)
		sha := gitObjectHash("commit", []byte(raw))
		commit := upgradeCommit{SHA: sha, Message: message}
		commit.Tree.SHA = tree
		commit.Parents = append(commit.Parents, struct {
			SHA string `json:"sha"`
		}{parent})
		f.data[upgradeRepo+"/git/commits/"+sha] = commit
	case upgradeRepo + "/git/refs":
		body := input.(map[string]string)
		branch := strings.TrimPrefix(body["ref"], "refs/heads/")
		key := upgradeRepo + "/git/ref/heads/" + url.PathEscape(branch)
		if _, exists := f.data[key]; exists {
			return errors.New("REF_ALREADY_EXISTS")
		}
		ref := reference{Ref: body["ref"]}
		ref.Object.SHA = body["sha"]
		f.data[key] = ref
	case upgradeRepo + "/pulls":
		body := input.(map[string]any)
		pull := upgradePull{Number: 9, State: "open",
			URL: "https://github.com/example/project/pull/9", Body: body["body"].(string)}
		pull.Head.Ref = body["head"].(string)
		ref := f.data[upgradeRepo+"/git/ref/heads/"+
			url.PathEscape(pull.Head.Ref)].(reference)
		pull.Head.SHA = ref.Object.SHA
		pull.Base.Ref = body["base"].(string)
		baseRef := f.data[upgradeRepo+"/git/ref/heads/"+
			url.PathEscape(pull.Base.Ref)]
		baseData, _ := json.Marshal(baseRef)
		var observedBase reference
		if err := json.Unmarshal(baseData, &observedBase); err != nil {
			return err
		}
		pull.Base.SHA = observedBase.Object.SHA
		repo := f.data[upgradeRepo].(Repository)
		pull.Head.Repository, pull.Base.Repository = repo, repo
		f.data[upgradeRepo+"/pulls/9"] = pull
		f.data[pullListPath(pull.Head.Ref)] = []upgradePull{pull}
	default:
		return errors.New("UNEXPECTED_MUTATION")
	}
	if f.afterWrite != nil {
		f.afterWrite(path)
	}
	if path == f.lost {
		f.denyRead = f.denyAfterWrite
		return errors.New("LOST_RESPONSE")
	}
	return nil
}

func pullListPath(branch string) string {
	return upgradeRepo + "/pulls?state=all&per_page=100&head=" +
		url.QueryEscape("example:"+branch)
}

func fixtureTree(f *fakeAPI, entries map[string]upgradeEntry) string {
	for path, entry := range entries {
		if entry.Type == "tree" {
			entry.SHA = treeHash(entries, path+"/")
			entries[path] = entry
		}
	}
	sha := treeHash(entries, "")
	tree := upgradeTree{SHA: sha}
	for _, entry := range entries {
		tree.Entries = append(tree.Entries, entry)
	}
	sort.Slice(tree.Entries, func(i, j int) bool {
		return tree.Entries[i].Path < tree.Entries[j].Path
	})
	f.data[upgradeRepo+"/git/trees/"+sha+"?recursive=1"] = tree
	return sha
}

func upgradeRecipeFixture(unified bool) map[string][]string {
	recipes := map[string][]string{
		"wake": {"name: Core CORE_COMMIT", ""},
		"issue": {"name: Nibfin issue", "on: [issues]", "permissions:",
			"  contents: read", "jobs:", "  observe:",
			"    runs-on: [self-hosted, nibfin-user-MEMBER_ID]", ""},
		"action-wake": {"name: Nibfin wake", "on: [workflow_dispatch]", "jobs:",
			"  wake:", "    runs-on: [self-hosted, nibfin-user-MEMBER_ID]",
			"    steps:", "      - uses: taco3064/nibfin-act@ACTION_COMMIT", ""},
	}
	if unified {
		recipes["action-dispatch"] = append([]string{}, recipes["action-wake"]...)
		recipes["action-dispatch"][0] = "name: Nibfin unified dispatch"
	}
	return recipes
}

func fixtureProducers(t *testing.T, f *fakeAPI, core, action string,
	artifactID int64, unified bool) Producers {
	t.Helper()
	pins := Producers{Core: SourcePin{Repository: officialCore, Commit: core},
		Action: SourcePin{Repository: officialAction, Commit: action}}
	recipes, _ := json.Marshal(upgradeRecipeFixture(unified))
	manifest := []byte(`{"schemaVersion":1,"fixture":"controlled source manifest"}`)
	pins.Core.ManifestHash = sha256Hash(manifest)
	files := map[string][]byte{"bin/nibfin-core.mjs": []byte("throw 'never execute';\n"),
		".node-version": []byte("24.22.1\n"), "manifest.json": manifest,
		"src/caller-recipes.json": recipes}
	pins.Artifact.ID = artifactID
	fixtureArchive(t, f, &pins, files)
	pinJSON, _ := json.Marshal(pins.Core)
	for _, pin := range []SourcePin{pins.Core, pins.Action} {
		f.data["repos/"+pin.Repository] = Repository{FullName: pin.Repository, ID: artifactID}
		f.data["repos/"+pin.Repository+"/commits/"+pin.Commit] =
			map[string]string{"sha": pin.Commit}
	}
	f.data["repos/"+officialAction+"/contents/core-source.json?ref="+action] =
		encoded(pinJSON)
	f.data["repos/"+officialCore+"/contents/src/caller-recipes.json?ref="+core] =
		encoded(recipes)
	return pins
}

func upgradeFixture(t *testing.T, oldUnified, nextUnified bool) *upgradeAPI {
	t.Helper()
	f := &upgradeAPI{fakeAPI: initFixture()}
	f.old = fixtureProducers(t, f.fakeAPI, strings.Repeat("1", 40),
		strings.Repeat("2", 40), 101, oldUnified)
	f.target = fixtureProducers(t, f.fakeAPI, strings.Repeat("3", 40),
		strings.Repeat("4", 40), 102, nextUnified)
	f.installed = templates(f.old, 42, upgradeRecipeFixture(oldUnified))
	f.baseEntries = map[string]upgradeEntry{}
	for path, text := range f.installed {
		f.baseEntries[path] = upgradeEntry{Path: path, Mode: "100644",
			Type: "blob", SHA: blobHash([]byte(text))}
		f.data[upgradeRepo+"/contents/"+path+"?ref="+strings.Repeat("a", 40)] =
			encoded([]byte(text))
	}
	for _, path := range []string{".nibfin", ".github", ".github/workflows"} {
		f.baseEntries[path] = upgradeEntry{Path: path, Mode: "040000", Type: "tree"}
	}
	f.baseEntries["user.txt"] = upgradeEntry{Path: "user.txt", Mode: "100755",
		Type: "blob", SHA: blobHash([]byte("unrelated user program"))}
	sha := fixtureTree(f.fakeAPI, f.baseEntries)
	commit := upgradeCommit{SHA: strings.Repeat("a", 40)}
	commit.Tree.SHA = sha
	f.data[upgradeRepo+"/git/commits/"+commit.SHA] = commit
	f.data["repos/"+officialCLI] = Repository{FullName: officialCLI, ID: 333}
	f.data["repos/"+officialCLI+"/releases/latest"] = map[string]any{
		"id": 1, "tag_name": "v1.2.3",
		"html_url": "https://github.com/" + officialCLI + "/releases/tag/v1.2.3"}
	f.data["repos/"+officialCLI+"/releases/tags/v1.2.3"] =
		f.data["repos/"+officialCLI+"/releases/latest"]
	cliCommit := strings.Repeat("5", 40)
	f.data["repos/"+officialCLI+"/git/ref/tags/v1.2.3"] = map[string]any{
		"ref": "refs/tags/v1.2.3", "object": map[string]string{
			"type": "commit", "sha": cliCommit}}
	f.data["repos/"+officialCLI+"/commits/"+cliCommit] =
		map[string]string{"sha": cliCommit}
	pins, _ := json.Marshal(f.target)
	f.data["repos/"+officialCLI+"/contents/internal/cli/producers.json?ref="+
		cliCommit] = encoded(pins)
	return f
}

func fixtureUpgradePlan(t *testing.T, f *upgradeAPI) upgradePlan {
	t.Helper()
	plan, err := planUpgrade(f, "example/project", "latest")
	if err != nil {
		t.Fatal(err)
	}
	f.data[pullListPath(upgradeBranchName)] = []upgradePull{}
	return plan
}

func fixtureArchive(t *testing.T, f *fakeAPI, pins *Producers,
	files map[string][]byte) {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0644,
			Typeflag: tar.TypeReg, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if tarWriter.Close() != nil || gzipWriter.Close() != nil {
		t.Fatal("archive")
	}
	archive := compressed.Bytes()
	pins.Core.ArchiveHash = sha256Hash(archive)
	pinJSON, _ := json.Marshal(pins.Core)
	var zipped bytes.Buffer
	writer := zip.NewWriter(&zipped)
	for name, data := range map[string][]byte{"nibfin-core.tar.gz": archive,
		"source-pin.json": pinJSON, "SHA256SUMS": []byte(pins.Core.ArchiveHash)} {
		stream, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if writer.Close() != nil {
		t.Fatal("zip")
	}
	pins.Artifact.Digest = "sha256:" + sha256Hash(zipped.Bytes())
	artifact := fmt.Sprintf("repos/%s/actions/artifacts/%d", officialCore, pins.Artifact.ID)
	f.data[artifact] = map[string]any{"id": pins.Artifact.ID, "digest": pins.Artifact.Digest}
	f.data[artifact+"/zip"] = zipped.Bytes()
}
