package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type sharedPlatform struct {
	Schema        int                             `json:"schemaVersion"`
	RunnerVersion string                          `json:"runnerVersion"`
	NodeVersion   string                          `json:"nodeVersion"`
	Targets       map[string]sharedPlatformRecord `json:"targets"`
}

type sharedPlatformRecord struct {
	ArtifactArch    string   `json:"artifactArch"`
	Observations    []string `json:"observations"`
	ELFMachine      uint16   `json:"elfMachine"`
	RunnerHash      string   `json:"runnerArchiveSha256"`
	ListenerHash    string   `json:"listenerSha256"`
	WorkerHash      string   `json:"workerSha256"`
	NodeArchiveHash string   `json:"nodeArchiveSha256"`
	NodeHash        string   `json:"nodeSha256"`
}

type sharedTarget struct {
	sharedPlatformRecord
	Arch, RunnerVersion, NodeVersion string
}

func (t sharedTarget) nodeDirectory() string {
	return "node-v" + t.NodeVersion + "-linux-" + t.ArtifactArch
}
func (t sharedTarget) nodePath() string {
	return "/opt/nibfin/tools/" + t.nodeDirectory() + "/bin/node"
}
func (t sharedTarget) nodeURL() string {
	return "https://nodejs.org/dist/v" + t.NodeVersion + "/" +
		t.nodeDirectory() + ".tar.xz"
}
func (t sharedTarget) runnerFilename() string {
	return "actions-runner-linux-" + t.ArtifactArch + "-" + t.RunnerVersion + ".tar.gz"
}
func (t sharedTarget) runnerURL() string {
	return "https://github.com/actions/runner/releases/download/v" +
		t.RunnerVersion + "/" + t.runnerFilename()
}

var platformVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
var platformAlias = regexp.MustCompile(`^[a-z0-9_]+$`)

// JSON struct decoding alone accepts duplicate and case-insensitive keys.
// Producer identities require the exact finite schema instead.
func platformObject(data []byte, keys ...string) (map[string]json.RawMessage, error) {
	invalid := errors.New("INVALID_SHARED_PLATFORM")
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, invalid
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, invalid
	}
	if len(fields) != len(keys) || decoder.Decode(new(any)) != io.EOF {
		return nil, invalid
	}
	return fields, nil
}

func parseSharedPlatform(data, version []byte) (sharedPlatform, error) {
	var c sharedPlatform
	invalid := errors.New("INVALID_SHARED_PLATFORM")
	if len(data) > 16384 || len(version) > 64 {
		return c, invalid
	}
	fields, err := platformObject(data,
		"schemaVersion", "runnerVersion", "nodeVersion", "targets")
	if err != nil {
		return c, invalid
	}
	targets, err := platformObject(fields["targets"], "amd64", "arm64")
	if err != nil {
		return c, invalid
	}
	for _, raw := range targets {
		_, err := platformObject(raw, "artifactArch", "observations", "elfMachine",
			"runnerArchiveSha256", "listenerSha256", "workerSha256",
			"nodeArchiveSha256", "nodeSha256")
		if err != nil {
			return c, invalid
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF ||
		c.Schema != 1 || c.NodeVersion != strings.TrimSpace(string(version)) ||
		!platformVersion.MatchString(c.NodeVersion) ||
		!platformVersion.MatchString(c.RunnerVersion) || len(c.Targets) != 2 {
		return c, invalid
	}
	aliases := map[string]bool{}
	for arch, machine := range map[string]uint16{"amd64": 62, "arm64": 183} {
		r, ok := c.Targets[arch]
		artifact := arch
		if arch == "amd64" {
			artifact = "x64"
		}
		if !ok || r.ArtifactArch != artifact || r.ELFMachine != machine ||
			len(r.Observations) < 2 || len(r.Observations) > 8 {
			return c, invalid
		}
		hasArch, hasArtifact := false, false
		for _, alias := range r.Observations {
			if len(alias) > 32 || !platformAlias.MatchString(alias) || aliases[alias] {
				return c, invalid
			}
			aliases[alias] = true
			hasArch = hasArch || alias == arch
			hasArtifact = hasArtifact || alias == artifact
		}
		if !hasArch || !hasArtifact {
			return c, invalid
		}
		for _, hash := range []string{r.RunnerHash, r.ListenerHash, r.WorkerHash,
			r.NodeArchiveHash, r.NodeHash} {
			if !digestPattern.MatchString(hash) {
				return c, invalid
			}
		}
	}
	return c, nil
}

// The caller supplies the already hash-verified producer archive. No producer
// module or cached executable is executed to discover bootstrap identities.
func archivedSharedPlatform(archive []byte) (sharedPlatform, error) {
	files, err := readCoreArchive(archive)
	if err != nil {
		return sharedPlatform{}, err
	}
	return parseSharedPlatform(files["src/shared-platform.json"].data,
		files[".node-version"].data)
}

func verifiedSharedPlatform(api API) (sharedPlatform, error) {
	archive, err := candidateArchive(api, Pins())
	if err != nil {
		return sharedPlatform{}, err
	}
	return archivedSharedPlatform(archive)
}

func (c sharedPlatform) selectTarget(kernel, runtime string, machine uint16) (
	sharedTarget, error,
) {
	canonical := func(observed string) string {
		for arch, record := range c.Targets {
			for _, alias := range record.Observations {
				if alias == observed {
					return arch
				}
			}
		}
		return ""
	}
	arch := canonical(kernel)
	record, ok := c.Targets[arch]
	if !ok {
		return sharedTarget{}, errors.New("UNSUPPORTED_SHARED_PLATFORM")
	}
	if canonical(runtime) != arch || machine != record.ELFMachine {
		return sharedTarget{}, errors.New("SHARED_PLATFORM_MISMATCH")
	}
	return sharedTarget{record, arch, c.RunnerVersion, c.NodeVersion}, nil
}

func sharedELFMachine(header []byte) (uint16, error) {
	if len(header) < 64 || string(header[:4]) != "\x7fELF" || header[4] != 2 ||
		header[5] != 1 || header[6] != 1 ||
		(binary.LittleEndian.Uint16(header[16:]) != 2 &&
			binary.LittleEndian.Uint16(header[16:]) != 3) ||
		binary.LittleEndian.Uint32(header[20:]) != 1 {
		return 0, errors.New("INVALID_SHARED_EXECUTABLE")
	}
	return binary.LittleEndian.Uint16(header[18:]), nil
}

type sharedInstallation struct {
	archive  []byte
	platform sharedPlatform
	target   sharedTarget
}

func sharedInstallationFromProducer(api API) (sharedInstallation, error) {
	return sharedInstallationForProducers(api, Pins())
}

func sharedInstallationForProducers(api API, pins Producers) (sharedInstallation, error) {
	archive, err := candidateArchive(api, pins)
	if err != nil {
		return sharedInstallation{}, err
	}
	contract, err := archivedSharedPlatform(archive)
	if err != nil {
		return sharedInstallation{}, err
	}
	target, err := nativeSharedTarget(contract)
	if err != nil {
		return sharedInstallation{}, err
	}
	return sharedInstallation{archive, contract, target}, nil
}

func sharedTargetFromProducer(api API) (sharedTarget, error) {
	c, err := verifiedSharedPlatform(api)
	if err != nil {
		return sharedTarget{}, err
	}
	return nativeSharedTarget(c)
}

func selectSharedRunner(api API, target sharedTarget) (RunnerApplication, error) {
	var release struct {
		Tag    string `json:"tag_name"`
		Draft  bool   `json:"draft"`
		Assets []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
			URL    string `json:"browser_download_url"`
		} `json:"assets"`
	}
	endpoint := fmt.Sprintf("repos/actions/runner/releases/tags/v%s", target.RunnerVersion)
	if err := api.Call("GET", endpoint, nil, &release); err != nil {
		return RunnerApplication{}, err
	}
	matches := 0
	var app RunnerApplication
	for _, asset := range release.Assets {
		if asset.Name != target.runnerFilename() {
			continue
		}
		matches++
		if asset.Digest == "sha256:"+target.RunnerHash && asset.URL == target.runnerURL() {
			app = RunnerApplication{OS: "linux", Arch: target.ArtifactArch,
				Filename: asset.Name, Hash: target.RunnerHash, URL: asset.URL}
		}
	}
	if release.Tag != "v"+target.RunnerVersion || release.Draft ||
		matches != 1 || app.Hash == "" {
		return RunnerApplication{}, errors.New("OFFICIAL_SHARED_RUNNER_SOURCE_UNAVAILABLE")
	}
	return app, nil
}
