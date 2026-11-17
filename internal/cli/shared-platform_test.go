package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// These synthetic identities exercise transport and selection, not support
// certification or the availability of the real producer's release artifacts.
func platformFixture() sharedPlatform {
	c := sharedPlatform{Schema: 1, RunnerVersion: "1.0.0", NodeVersion: "1.2.3",
		Targets: map[string]sharedPlatformRecord{}}
	for arch, machine := range map[string]uint16{"amd64": 62, "arm64": 183} {
		artifact, aliases := arch, []string{"arm64", "aarch64"}
		if arch == "amd64" {
			artifact, aliases = "x64", []string{"amd64", "x64", "x86_64"}
		}
		c.Targets[arch] = sharedPlatformRecord{ArtifactArch: artifact, Observations: aliases,
			ELFMachine: machine, RunnerHash: strings.Repeat("a", 64),
			ListenerHash: strings.Repeat("b", 64), WorkerHash: strings.Repeat("c", 64),
			NodeArchiveHash: strings.Repeat("d", 64), NodeHash: strings.Repeat("e", 64)}
	}
	return c
}

func platformArchiveFixture(t *testing.T, c sharedPlatform) []byte {
	t.Helper()
	data, _ := json.Marshal(c)
	return platformArchiveFiles(t, map[string][]byte{
		"src/shared-platform.json": data, ".node-version": []byte(c.NodeVersion + "\n"),
		"bin/nibfin-core.mjs": []byte("synthetic Core source"),
	})
}

func platformArchiveFiles(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for name, data := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644,
			Typeflag: tar.TypeReg, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if writer.Close() != nil || compressed.Close() != nil {
		t.Fatal("fixture archive")
	}
	return buffer.Bytes()
}

func installSharedProducerFixture(t *testing.T, api *fakeAPI, c sharedPlatform) {
	t.Helper()
	old := bytes.Clone(producerJSON)
	t.Cleanup(func() { producerJSON = old })
	pins := Pins()
	archive := platformArchiveFixture(t, c)
	pins.Core.ArchiveHash = sha256Hash(archive)
	pin, _ := json.Marshal(pins.Core)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, data := range map[string][]byte{
		"nibfin-core.tar.gz": archive, "source-pin.json": pin, "SHA256SUMS": []byte("test"),
	} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if writer.Close() != nil {
		t.Fatal("fixture zip")
	}
	pins.Artifact.Digest = "sha256:" + sha256Hash(buffer.Bytes())
	producerJSON, _ = json.Marshal(pins)
	endpoint := fmt.Sprintf("repos/%s/actions/artifacts/%d", pins.Core.Repository,
		pins.Artifact.ID)
	api.data[endpoint] = map[string]any{"id": pins.Artifact.ID,
		"expired": false, "digest": pins.Artifact.Digest}
	api.data[endpoint+"/zip"] = buffer.Bytes()
	target := sharedTarget{c.Targets[runtime.GOARCH], runtime.GOARCH,
		c.RunnerVersion, c.NodeVersion}
	api.data["repos/actions/runner/releases/tags/v"+c.RunnerVersion] = map[string]any{
		"tag_name": "v" + c.RunnerVersion, "assets": []map[string]string{{
			"name": target.runnerFilename(), "digest": "sha256:" + target.RunnerHash,
			"browser_download_url": target.runnerURL(),
		}},
	}
}

func TestPlatformContractValidation(t *testing.T) {
	for _, mutate := range []func(*sharedPlatform){
		func(c *sharedPlatform) { c.Schema = 2 },
		func(c *sharedPlatform) { c.NodeVersion = "9.9.9" },
		func(c *sharedPlatform) { delete(c.Targets, "arm64") },
		func(c *sharedPlatform) {
			r := c.Targets["arm64"]
			r.ELFMachine = 62
			c.Targets["arm64"] = r
		},
		func(c *sharedPlatform) {
			r := c.Targets["arm64"]
			r.Observations = append(r.Observations, "x64")
			c.Targets["arm64"] = r
		},
		func(c *sharedPlatform) {
			r := c.Targets["amd64"]
			r.NodeHash = "bad"
			c.Targets["amd64"] = r
		},
	} {
		c := platformFixture()
		mutate(&c)
		data, _ := json.Marshal(c)
		if _, err := parseSharedPlatform(data, []byte("1.2.3")); err == nil {
			t.Fatal("invalid producer contract accepted")
		}
	}
	c := platformFixture()
	data, _ := json.Marshal(c)
	for _, raw := range [][]byte{
		append([]byte(`{"schemaVersion":1,`), data[1:]...),
		bytes.Replace(data, []byte(`"nodeVersion"`), []byte(`"NodeVersion"`), 1),
		append(data, []byte(`{}`)...),
	} {
		if _, err := parseSharedPlatform(raw, []byte("1.2.3")); err == nil {
			t.Fatal("ambiguous or non-finite schema accepted")
		}
	}
	if _, err := archivedSharedPlatform(platformArchiveFixture(t, c)); err != nil {
		t.Fatal(err)
	}
}

func TestPlatformSelectionAndArtifactPaths(t *testing.T) {
	c := platformFixture()
	for arch, r := range c.Targets {
		for _, alias := range r.Observations {
			target, err := c.selectTarget(alias, arch, r.ELFMachine)
			if err != nil || target.Arch != arch ||
				!strings.Contains(target.nodePath(), "-linux-"+r.ArtifactArch+"/") ||
				!strings.HasSuffix(target.nodeURL(), ".tar.xz") ||
				!strings.Contains(target.runnerFilename(), "-"+r.ArtifactArch+"-") {
				t.Fatalf("selection %s: %+v %v", alias, target, err)
			}
		}
	}
	for _, test := range []struct {
		kernel, runtime string
		elf             uint16
	}{
		{"x86_64", "arm64", 62}, {"aarch64", "arm64", 62},
		{"mips", "mips", 8}, {"x86_64", "unknown", 62},
	} {
		if _, err := c.selectTarget(test.kernel, test.runtime, test.elf); err == nil {
			t.Fatal("contradictory or unknown observations accepted")
		}
	}
}

func TestSharedRunnerReleaseRequiresOneExactAsset(t *testing.T) {
	c := platformFixture()
	for arch, r := range c.Targets {
		target := sharedTarget{r, arch, c.RunnerVersion, c.NodeVersion}
		endpoint := "repos/actions/runner/releases/tags/v" + c.RunnerVersion
		valid := map[string]string{"name": target.runnerFilename(),
			"digest": "sha256:" + r.RunnerHash, "browser_download_url": target.runnerURL()}
		for _, count := range []int{0, 1, 2} {
			api := initFixture()
			assets := []map[string]string{}
			for range count {
				assets = append(assets, valid)
			}
			api.data[endpoint] = map[string]any{"tag_name": "v" + c.RunnerVersion,
				"assets": assets}
			app, err := selectSharedRunner(api, target)
			if (err == nil) != (count == 1) || err == nil && app.Arch != r.ArtifactArch {
				t.Fatalf("asset count %d: %+v %v", count, app, err)
			}
		}
	}
}

func TestPlatformReaderAuthenticatesArchiveBeforeUsingContract(t *testing.T) {
	api := initFixture()
	installSharedProducerFixture(t, api, platformFixture())
	if _, err := verifiedSharedPlatform(api); err != nil {
		t.Fatal(err)
	}
	pins := Pins()
	endpoint := fmt.Sprintf("repos/%s/actions/artifacts/%d", pins.Core.Repository,
		pins.Artifact.ID)
	original := api.data[endpoint+"/zip"]
	api.data[endpoint+"/zip"] = []byte("tampered producer archive")
	if _, err := verifiedSharedPlatform(api); err == nil {
		t.Fatal("contract read from unverified archive")
	}
	api.data[endpoint+"/zip"] = original
	api.data[endpoint] = map[string]any{"id": pins.Artifact.ID, "expired": true,
		"digest": pins.Artifact.Digest}
	if _, err := verifiedSharedPlatform(api); err == nil {
		t.Fatal("expired producer used")
	}
}
