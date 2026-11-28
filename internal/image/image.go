// Package image resolves the Nibfin image user repositories run: the latest
// main image on GHCR, pinned by digest, once its attestation verifies.
package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/cli/go-gh/v2"

	"github.com/taco3064/gh-nibfin/internal/op"
)

const (
	// Repository is the image repository on GHCR.
	Repository = "ghcr.io/taco3064/nibfin"
	// SourceRepo is the repository whose workflow builds and attests the image.
	SourceRepo     = "taco3064/nibfin-core"
	signerWorkflow = SourceRepo + "/.github/workflows/image.yml"
	sourceRef      = "refs/heads/main"
	tag            = "main"
	name           = "taco3064/nibfin"
)

// Tests replace these.
var (
	registry   = "https://ghcr.io"
	httpClient = &http.Client{Timeout: 30 * time.Second}
	execGH     = gh.ExecContext
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// commitPattern is a full nibfin-core commit SHA.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// manifestTypes are the media types the main tag may resolve to.
var manifestTypes = []string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}

// Latest returns ghcr.io/taco3064/nibfin@sha256:<digest> for the image the main
// tag points to, after gh attestation verify accepts its attestation: SLSA
// provenance signed by nibfin-core's image.yml on refs/heads/main, built on a
// GitHub-hosted runner. It never falls back to another digest.
func Latest(ctx context.Context) (string, error) {
	ref, _, err := resolve(ctx)
	return ref, err
}

// LatestRelease is Latest plus the nibfin-core commit the image was built
// from: the Source Repository Digest (Fulcio extension
// 1.3.6.1.4.1.57264.1.13) in the signing certificate of the verified
// attestations, the value the collaborator runner hook also checks. Every
// verified attestation must name the same full commit; otherwise it is
// image_unverified.
func LatestRelease(ctx context.Context) (ref, commit string, err error) {
	ref, results, err := resolve(ctx)
	if err != nil {
		return "", "", err
	}
	if commit, err = sourceCommit(ref, results); err != nil {
		return "", "", err
	}
	return ref, commit, nil
}

// resolve reads the digest of the main tag and verifies its attestation.
func resolve(ctx context.Context) (string, []verified, error) {
	digest, err := mainDigest(ctx)
	if err != nil {
		return "", nil, err
	}
	ref := Repository + "@" + digest
	results, err := verify(ctx, ref)
	if err != nil {
		return "", nil, err
	}
	return ref, results, nil
}

// mainDigest reads the digest of the main tag with an anonymous pull token.
func mainDigest(ctx context.Context) (string, error) {
	token, err := pullToken(ctx)
	if err != nil {
		return "", err
	}
	u := registry + "/v2/" + name + "/manifests/" + tag
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return "", op.Wrap(op.CodeImageUnresolved, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join(manifestTypes, ", "))
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", op.Wrap(op.CodeImageUnresolved, fmt.Errorf("read %s:%s: %w", Repository, tag, err))
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", op.Errorf(op.CodeImageUnresolved, "read %s:%s: registry returned %s", Repository, tag, resp.Status)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if !digestPattern.MatchString(digest) {
		return "", op.Errorf(op.CodeImageUnresolved, "read %s:%s: registry returned digest %q", Repository, tag, digest)
	}
	return digest, nil
}

// pullToken gets an anonymous pull token from GHCR's token endpoint. The token
// is only sent back to the registry; it is never logged or returned.
func pullToken(ctx context.Context) (string, error) {
	u := registry + "/token?scope=repository:" + name + ":pull&service=ghcr.io"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", op.Wrap(op.CodeImageUnresolved, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", op.Wrap(op.CodeImageUnresolved, fmt.Errorf("get a pull token for %s: %w", Repository, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", op.Errorf(op.CodeImageUnresolved, "get a pull token for %s: registry returned %s", Repository, resp.Status)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Token == "" {
		return "", op.Errorf(op.CodeImageUnresolved, "get a pull token for %s: the registry returned no token", Repository)
	}
	return body.Token, nil
}

// verifyArgs are the gh arguments that verify ref's attestation.
func verifyArgs(ref string) []string {
	return []string{
		"attestation", "verify", "oci://" + ref,
		"--hostname", "github.com",
		"--repo", SourceRepo,
		"--signer-workflow", signerWorkflow,
		"--source-ref", sourceRef,
		"--deny-self-hosted-runners",
		"--format", "json",
	}
}

// verified is one entry of gh attestation verify's JSON, one verified
// attestation (gh's AttestationProcessingResult); only the signed source
// commit is read.
type verified struct {
	VerificationResult *struct {
		Signature struct {
			Certificate struct {
				SourceRepositoryDigest string `json:"sourceRepositoryDigest"`
			} `json:"certificate"`
		} `json:"signature"`
	} `json:"verificationResult"`
}

// verify runs gh attestation verify with the user's own gh login and returns
// the verified attestations.
func verify(ctx context.Context, ref string) ([]verified, error) {
	stdout, stderr, err := execGH(ctx, verifyArgs(ref)...)
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 4 && msg == "" {
			msg = "gh is not logged in; run gh auth login"
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, op.Errorf(op.CodeImageUnverified, "verify the attestation of %s: %s", ref, msg)
	}
	var results []verified
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &results); err != nil {
		return nil, op.Errorf(op.CodeImageUnverified, "verify the attestation of %s: gh returned no verification results: %v", ref, err)
	}
	if len(results) == 0 {
		return nil, op.Errorf(op.CodeImageUnverified, "verify the attestation of %s: gh verified no attestation", ref)
	}
	return results, nil
}

// sourceCommit returns the source commit every verified attestation of ref
// names; a missing, malformed or disagreeing one is image_unverified.
func sourceCommit(ref string, results []verified) (string, error) {
	commit := ""
	for i, r := range results {
		c := ""
		if r.VerificationResult != nil {
			c = r.VerificationResult.Signature.Certificate.SourceRepositoryDigest
		}
		if !commitPattern.MatchString(c) {
			return "", op.Errorf(op.CodeImageUnverified, "the verified attestation %d of %s names the source commit %q, not a full commit SHA", i+1, ref, c)
		}
		if commit != "" && c != commit {
			return "", op.Errorf(op.CodeImageUnverified, "the verified attestations of %s name two source commits, %s and %s", ref, commit, c)
		}
		commit = c
	}
	return commit, nil
}
