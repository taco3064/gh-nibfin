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
	"path"
	"strings"
	"testing"
)

const upgradeRepo = "repos/example/project"

type upgradeProvider struct {
	*fakeAPI
	files   map[string]map[string]string
	trees   map[string]map[string]string
	pulls   []upgradePR
	reads   []string
	lost    string
	hook    func(string, string)
	blocked string
}

func putJSON(f *fakeAPI, endpoint string, value any) {
	data, _ := json.Marshal(value)
	f.data[endpoint] = encoded(data)
}

func upgradePackage(t *testing.T, pins *Producers, marker string) ([]byte, string) {
	t.Helper()
	// Synthetic source, not a replacement for Core's official archive verifier.
	// Native Node executes this manifest-checking fixture from the verified archive.
	script := `import { readFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
const [, , verb, root, pinPath] = process.argv;
if (verb !== 'verify') process.exit(1);
const pin = JSON.parse(await readFile(pinPath));
const manifest = await readFile(root + '/source-manifest.json');
const hash = data => createHash('sha256').update(data).digest('hex');
if (hash(manifest) !== pin.manifestHash) process.exit(1);
for (const [name, digest] of Object.entries(JSON.parse(manifest))) {
  if (hash(await readFile(root + '/' + name)) !== digest) process.exit(1);
}
console.log(JSON.stringify({ status: 'verified', source: pin }));
`
	version, err := (NativeProcess{}).Run("node", []string{"--version"}, nil)
	if err != nil {
		t.Fatal("synthetic cache integration requires native Node: ", err)
	}
	files := map[string]string{"bin/nibfin-core.mjs": script,
		".node-version": strings.TrimPrefix(string(version), "v"), "marker": marker}
	manifest := map[string]string{}
	for name, body := range files {
		manifest[name] = sha256Hash([]byte(body))
	}
	data, _ := json.Marshal(manifest)
	files["source-manifest.json"] = string(data)
	pins.Core.ManifestHash = sha256Hash(data)
	var tarball bytes.Buffer
	compressed := gzip.NewWriter(&tarball)
	writer := tar.NewWriter(compressed)
	for name, body := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Size: int64(len(body)),
			Mode: 0644, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if writer.Close() != nil || compressed.Close() != nil {
		t.Fatal("tar fixture close")
	}
	pins.Core.ArchiveHash = sha256Hash(tarball.Bytes())
	var archive bytes.Buffer
	zipper := zip.NewWriter(&archive)
	pin, _ := json.Marshal(pins.Core)
	for name, body := range map[string][]byte{"source-pin.json": pin,
		"nibfin-core.tar.gz": tarball.Bytes(), "SHA256SUMS": []byte("fixture")} {
		file, err := zipper.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if zipper.Close() != nil {
		t.Fatal("zip fixture close")
	}
	pins.Artifact.Digest = "sha256:" + sha256Hash(archive.Bytes())
	return archive.Bytes(), script
}

func upgradeFixture(t *testing.T) (*upgradeProvider, upgradeSelection, Producers) {
	t.Helper()
	f := &upgradeProvider{fakeAPI: initFixture(), files: map[string]map[string]string{},
		trees: map[string]map[string]string{}}
	old, target := Pins(), Pins()
	old.Core.Commit, old.Action.Commit = strings.Repeat("1", 40), strings.Repeat("2", 40)
	target.Core.Commit = strings.Repeat("3", 40)
	target.Action.Commit = strings.Repeat("4", 40)
	old.Artifact.ID, target.Artifact.ID = 101, 102
	for _, pins := range []*Producers{&old, &target} {
		archive, _ := upgradePackage(t, pins, pins.Core.Commit)
		for _, pin := range []SourcePin{pins.Core, pins.Action} {
			f.data["repos/"+pin.Repository] = Repository{FullName: pin.Repository}
			f.data["repos/"+pin.Repository+"/commits/"+pin.Commit] =
				map[string]string{"sha": pin.Commit}
		}
		putJSON(f.fakeAPI, "repos/"+pins.Action.Repository+
			"/contents/core-source.json?ref="+pins.Action.Commit, pins.Core)
		recipes := recipeFixture()
		recipes["action-wake"] = []string{"name: ACTION_COMMIT nibfin-user-MEMBER_ID", ""}
		if pins == &target {
			recipes["action-dispatch"] = recipes["action-wake"]
		}
		putJSON(f.fakeAPI, "repos/"+pins.Core.Repository+
			"/contents/src/caller-recipes.json?ref="+pins.Core.Commit, recipes)
		artifact := fmt.Sprintf("repos/%s/actions/artifacts/%d",
			pins.Core.Repository, pins.Artifact.ID)
		f.data[artifact] = map[string]any{"id": pins.Artifact.ID,
			"expired": false, "digest": pins.Artifact.Digest}
		f.data[artifact+"/zip"] = archive
	}
	cli := strings.Repeat("5", 40)
	f.data["repos/"+officialCLI+"/commits/"+cli] = map[string]string{"sha": cli}
	putJSON(f.fakeAPI, "repos/"+officialCLI+
		"/contents/internal/cli/producers.json?ref="+cli, target)
	f.data["repos/"+officialCLI+"/releases/tags/v1.2.3"] = map[string]any{
		"id": 7, "tag_name": "v1.2.3", "draft": false, "prerelease": false,
		"html_url": "https://github.com/" + officialCLI + "/releases/tag/v1.2.3"}
	f.data["repos/"+officialCLI+"/git/ref/tags/v1.2.3"] = map[string]any{
		"ref": "refs/tags/v1.2.3", "object": map[string]string{"type": "commit", "sha": cli}}
	recipes := recipeFixture()
	recipes["action-wake"] = []string{"name: ACTION_COMMIT nibfin-user-MEMBER_ID", ""}
	files := templates(old, 42, recipes)
	files["unrelated"] = "preserve me"
	files[".nibfin/approvals.json"] = "original approved context"
	head := strings.Repeat("a", 40)
	f.installFiles(head, files)
	tree := f.storeTree(files)
	f.data[upgradeRepo+"/git/commits/"+head] = map[string]any{
		"sha": head, "tree": map[string]string{"sha": tree}}
	return f, upgradeSelection{Version: "v1.2.3"}, target
}

func (f *upgradeProvider) installFiles(sha string, files map[string]string) {
	f.files[sha] = files
	for name, body := range files {
		f.data[upgradeRepo+"/contents/"+name+"?ref="+sha] = encoded([]byte(body))
	}
}

func (f *upgradeProvider) storeTree(files map[string]string) string {
	groups := map[string][]upgradeTreeEntry{}
	var build func(string) string
	build = func(directory string) string {
		children := map[string]bool{}
		var entries []upgradeTreeEntry
		for name, body := range files {
			if !strings.HasPrefix(name, directory) {
				continue
			}
			relative := strings.TrimPrefix(name, directory)
			parts := strings.SplitN(relative, "/", 2)
			if len(parts) == 2 {
				children[parts[0]] = true
			} else {
				entries = append(entries, upgradeTreeEntry{Path: relative,
					Mode: "100644", Type: "blob", SHA: blobHash([]byte(body))})
			}
		}
		for child := range children {
			entries = append(entries, upgradeTreeEntry{Path: child, Mode: "040000",
				Type: "tree", SHA: build(directory + child + "/")})
		}
		sha := treeHash(entries)
		groups[directory] = entries
		f.data[upgradeRepo+"/git/trees/"+sha] = upgradeTree{SHA: sha, Entries: entries}
		return sha
	}
	sha := build("")
	var recursive []upgradeTreeEntry
	for directory, entries := range groups {
		for _, entry := range entries {
			entry.Path = directory + entry.Path
			recursive = append(recursive, entry)
		}
	}
	f.data[upgradeRepo+"/git/trees/"+sha+"?recursive=1"] =
		upgradeTree{SHA: sha, Entries: recursive}
	f.trees[sha] = files
	return sha
}

func (f *upgradeProvider) Call(method, endpoint string, input, output any) error {
	if f.hook != nil {
		f.hook(method, endpoint)
	}
	if strings.Contains(endpoint, f.blocked) && f.blocked != "" {
		return errors.New("READBACK_UNAVAILABLE")
	}
	if method == "GET" {
		f.reads = append(f.reads, endpoint)
		if strings.HasPrefix(endpoint, upgradeRepo+"/pulls?") {
			data, _ := json.Marshal(f.pulls)
			return json.Unmarshal(data, output)
		}
		return f.fakeAPI.Call(method, endpoint, input, output)
	}
	f.writes = append(f.writes, method+" "+endpoint)
	switch endpoint {
	case upgradeRepo + "/git/trees":
		body := input.(map[string]any)
		files := map[string]string{}
		for name, content := range f.trees[body["base_tree"].(string)] {
			files[name] = content
		}
		for _, entry := range body["tree"].([]map[string]any) {
			if value, ok := entry["content"]; ok {
				files[entry["path"].(string)] = value.(string)
			} else {
				delete(files, entry["path"].(string))
			}
		}
		f.storeTree(files)
	case upgradeRepo + "/git/commits":
		body := input.(map[string]any)
		tree, parent := body["tree"].(string), body["parents"].([]string)[0]
		message := body["message"].(string)
		identity := "Nibfin upgrade <noreply@github.com> 946684800 +0000"
		raw := fmt.Sprintf("tree %s\nparent %s\nauthor %s\ncommitter %s\n\n%s",
			tree, parent, identity, identity, message)
		sha := gitObjectHash("commit", []byte(raw))
		f.data[upgradeRepo+"/git/commits/"+sha] = map[string]any{
			"sha": sha, "tree": map[string]string{"sha": tree}, "message": message,
			"parents": []map[string]string{{"sha": parent}},
			"author":  body["author"], "committer": body["committer"]}
		f.installFiles(sha, f.trees[tree])
	case upgradeRepo + "/git/refs":
		body := input.(map[string]string)
		branch := strings.TrimPrefix(body["ref"], "refs/heads/")
		f.data[upgradeRepo+"/git/ref/heads/"+url.PathEscape(branch)] =
			map[string]any{"ref": body["ref"], "object": map[string]string{"sha": body["sha"]}}
	case upgradeRepo + "/pulls":
		body := input.(map[string]string)
		ref := f.data[upgradeRepo+"/git/ref/heads/"+url.PathEscape(body["head"])]
		data, _ := json.Marshal(ref)
		var branch reference
		_ = json.Unmarshal(data, &branch)
		pull := upgradePR{Number: 9, State: "open",
			URL: "https://github.com/example/project/pull/9"}
		pull.Head.Ref, pull.Head.SHA = body["head"], branch.Object.SHA
		pull.Base.Ref, pull.Base.SHA = body["base"], strings.Repeat("a", 40)
		for _, ref := range []*upgradePRRef{&pull.Head, &pull.Base} {
			ref.Repo.ID, ref.Repo.FullName = 10, "example/project"
		}
		f.pulls = append(f.pulls, pull)
		f.data[upgradeRepo+"/pulls/9"] = pull
	default:
		return errors.New("UNEXPECTED_MUTATION: " + path.Base(endpoint))
	}
	if strings.Contains(endpoint, f.lost) && f.lost != "" {
		return errors.New("RESPONSE_LOST")
	}
	return nil
}
