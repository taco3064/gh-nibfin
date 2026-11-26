// Package runnerhook builds the job-started hook of a collaborator's Runner:
// the files runner add writes to the host's hook directory. job-started.sh
// and check.mjs are constant; the official workflow files are this
// gh-nibfin's templates rendered with a placeholder image digest; the
// trusted root is the Sigstore Public Good root as gh obtains it. The
// repository's own values (the config) and GitHub's SSH host keys are added
// by internal/op, which receives these files through op.SetRunnerHookFiles.
//
// The package imports nothing of gh nibfin but the template files, so op
// (which templates and image import) can use it without an import cycle.
package runnerhook

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2"

	"github.com/taco3064/gh-nibfin/internal/templates/workflowfiles"
)

//go:embed files/job-started.sh files/check.mjs
var embedded embed.FS

// The hook's file names within the hook directory.
const (
	Script      = "job-started.sh"
	Check       = "check.mjs"
	Config      = "config"
	TrustedRoot = "trusted_root.json"
	KnownHosts  = "known_hosts"
	// OfficialDir holds the official workflow files.
	OfficialDir = "official"
)

// The image policy the hook applies, the same as internal/image's (a test
// keeps them equal; image imports op, so it cannot be imported here).
const (
	ImageRepository = "ghcr.io/taco3064/nibfin"
	SourceRepo      = "taco3064/nibfin-core"
	SignerWorkflow  = SourceRepo + "/.github/workflows/image.yml"
	SourceRef       = "refs/heads/main"
)

// PlaceholderImage is the image the official files are rendered with; the
// hook replaces a repository's digest with it before comparing.
const PlaceholderImage = ImageRepository + "@sha256:0000000000000000000000000000000000000000000000000000000000000000"

// Workflows are the files the hook compares, by name.
var Workflows = []string{"nibfin-shared-runner.yml", "nibfin-job.yml"}

// The Sigstore Public Good instance, the one nibfin-core's attestations use.
const (
	publicGoodCA  = "https://fulcio.sigstore.dev"
	publicGoodLog = "https://rekor.sigstore.dev"
)

// execGH runs gh; tests replace it.
var execGH = gh.ExecContext

// Static returns the constant files: the scripts and the official workflow
// files, keyed by their path in the hook directory.
func Static() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range []string{Script, Check} {
		b, err := embedded.ReadFile("files/" + name)
		if err != nil {
			return nil, err
		}
		out[name] = b
	}
	for _, name := range Workflows {
		b, err := workflowfiles.Render(name, map[string]string{"Image": PlaceholderImage})
		if err != nil {
			return nil, err
		}
		out[OfficialDir+"/"+name] = b
	}
	return out, nil
}

// Files returns Static's files and the trusted root: the Sigstore Public
// Good line of gh attestation trusted-root, which needs no login.
func Files(ctx context.Context) (map[string][]byte, error) {
	out, err := Static()
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := execGH(ctx, "attestation", "trusted-root")
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh attestation trusted-root: %s", msg)
	}
	root, err := PublicGood(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	out[TrustedRoot] = root
	return out, nil
}

// PublicGood picks the trusted root of the Sigstore Public Good instance from
// gh attestation trusted-root's JSON lines: the one whose certificate
// authorities include fulcio.sigstore.dev and whose logs include
// rekor.sigstore.dev. It returns that line with a final newline.
func PublicGood(jsonl []byte) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(jsonl))
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var root struct {
			CertificateAuthorities []struct {
				URI string `json:"uri"`
			} `json:"certificateAuthorities"`
			Tlogs []struct {
				BaseURL string `json:"baseUrl"`
			} `json:"tlogs"`
		}
		if err := json.Unmarshal(line, &root); err != nil {
			return nil, fmt.Errorf("gh attestation trusted-root printed a line that is not JSON: %v", err)
		}
		ca, log := false, false
		for _, c := range root.CertificateAuthorities {
			ca = ca || c.URI == publicGoodCA
		}
		for _, l := range root.Tlogs {
			log = log || l.BaseURL == publicGoodLog
		}
		if ca && log {
			return append(append([]byte{}, line...), '\n'), nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("gh attestation trusted-root printed no Sigstore Public Good root (" + publicGoodCA + ", " + publicGoodLog + ")")
}
