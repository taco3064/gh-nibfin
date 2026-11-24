package image

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
)

const digest = "sha256:47e9f06d0000000000000000000000000000000000000000000000000000abcd"

// fakeRegistry serves GHCR's token endpoint and the main manifest.
type fakeRegistry struct {
	digest      string
	tokenStatus int
	headStatus  int
	sawAuth     string
	sawAccept   string
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/token":
		if r.URL.Query().Get("scope") != "repository:taco3064/nibfin:pull" || r.URL.Query().Get("service") != "ghcr.io" {
			http.Error(w, "bad scope", http.StatusBadRequest)
			return
		}
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			return
		}
		w.Write([]byte(`{"token":"anon-pull"}`))
	case r.Method == http.MethodHead && r.URL.Path == "/v2/taco3064/nibfin/manifests/main":
		f.sawAuth, f.sawAccept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		if f.headStatus != 0 {
			w.WriteHeader(f.headStatus)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
		w.Header().Set("Docker-Content-Digest", f.digest)
	default:
		http.NotFound(w, r)
	}
}

type ghCall struct {
	stdout, stderr string
	err            error
	args           []string
}

func setup(t *testing.T, reg *fakeRegistry, gh *ghCall) {
	t.Helper()
	srv := httptest.NewServer(reg)
	t.Cleanup(srv.Close)
	oldReg, oldClient, oldExec := registry, httpClient, execGH
	t.Cleanup(func() { registry, httpClient, execGH = oldReg, oldClient, oldExec })
	registry, httpClient = srv.URL, srv.Client()
	execGH = func(_ context.Context, args ...string) (stdout, stderr bytes.Buffer, err error) {
		gh.args = args
		stdout.WriteString(gh.stdout)
		stderr.WriteString(gh.stderr)
		return stdout, stderr, gh.err
	}
}

func TestLatest(t *testing.T) {
	reg := &fakeRegistry{digest: digest}
	gh := &ghCall{stdout: `[{"verificationResult":{}}]` + "\n"}
	setup(t, reg, gh)
	ref, err := Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if want := "ghcr.io/taco3064/nibfin@" + digest; ref != want {
		t.Errorf("Latest = %q, want %q", ref, want)
	}
	wantArgs := []string{
		"attestation", "verify", "oci://ghcr.io/taco3064/nibfin@" + digest,
		"--hostname", "github.com",
		"--repo", "taco3064/nibfin-core",
		"--signer-workflow", "taco3064/nibfin-core/.github/workflows/image.yml",
		"--source-ref", "refs/heads/main",
		"--deny-self-hosted-runners",
		"--format", "json",
	}
	if !slices.Equal(gh.args, wantArgs) {
		t.Errorf("gh args\n %q\nwant\n %q", gh.args, wantArgs)
	}
	if reg.sawAuth != "Bearer anon-pull" {
		t.Errorf("manifest request Authorization %q", reg.sawAuth)
	}
	for _, mt := range manifestTypes {
		if !strings.Contains(reg.sawAccept, mt) {
			t.Errorf("Accept %q lacks %s", reg.sawAccept, mt)
		}
	}
}

func TestLatestFailures(t *testing.T) {
	// A real exit status 4, from this test binary run as a helper process.
	helper := exec.Command(os.Args[0], "-test.run=^TestHelperExit4$")
	helper.Env = append(os.Environ(), "NIBFIN_TEST_EXIT4=1")
	exit4 := helper.Run()
	var exitErr *exec.ExitError
	if !errors.As(exit4, &exitErr) || exitErr.ExitCode() != 4 {
		t.Fatalf("helper exit: %v", exit4)
	}
	ok := `[{"verificationResult":{}}]`
	cases := map[string]struct {
		reg      fakeRegistry
		gh       ghCall
		code     string
		contains string
		noGH     bool
	}{
		"token refused": {reg: fakeRegistry{digest: digest, tokenStatus: 503}, gh: ghCall{stdout: ok}, code: op.CodeImageUnresolved, noGH: true},
		"tag missing":   {reg: fakeRegistry{digest: digest, headStatus: 404}, gh: ghCall{stdout: ok}, code: op.CodeImageUnresolved, noGH: true},
		"no digest":     {reg: fakeRegistry{}, gh: ghCall{stdout: ok}, code: op.CodeImageUnresolved, noGH: true},
		"bad digest":    {reg: fakeRegistry{digest: "sha256:ABC; rm -rf /"}, gh: ghCall{stdout: ok}, code: op.CodeImageUnresolved, noGH: true},
		"verify fails":  {reg: fakeRegistry{digest: digest}, gh: ghCall{stderr: "Error: verifying with issuer \"sigstore.dev\"\n", err: errors.New("gh execution failed: exit status 1")}, code: op.CodeImageUnverified, contains: `verifying with issuer "sigstore.dev"`},
		"not logged in": {reg: fakeRegistry{digest: digest}, gh: ghCall{stderr: "To get started with GitHub CLI, please run:  gh auth login\n", err: exit4}, code: op.CodeImageUnverified, contains: "gh auth login"},
		"silent exit 4": {reg: fakeRegistry{digest: digest}, gh: ghCall{err: exit4}, code: op.CodeImageUnverified, contains: "gh auth login"},
		"empty results": {reg: fakeRegistry{digest: digest}, gh: ghCall{stdout: "[]\n"}, code: op.CodeImageUnverified, contains: "no attestation"},
		"not json":      {reg: fakeRegistry{digest: digest}, gh: ghCall{stdout: "verified\n"}, code: op.CodeImageUnverified},
		"no output":     {reg: fakeRegistry{digest: digest}, gh: ghCall{}, code: op.CodeImageUnverified},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			reg, gh := c.reg, c.gh
			setup(t, &reg, &gh)
			ref, err := Latest(context.Background())
			if ref != "" {
				t.Errorf("returned %q despite the failure", ref)
			}
			if got := op.Code(err); got != c.code {
				t.Errorf("code %q (%v), want %s", got, err, c.code)
			}
			if c.contains != "" && (err == nil || !strings.Contains(err.Error(), c.contains)) {
				t.Errorf("error %v does not carry %q", err, c.contains)
			}
			if err != nil && strings.Contains(err.Error(), "anon-pull") {
				t.Errorf("error exposes the pull token: %v", err)
			}
			if c.noGH && gh.args != nil {
				t.Errorf("gh ran after a registry failure: %q", gh.args)
			}
		})
	}
}

// TestHelperExit4 exits with status 4 when run as the helper of
// TestLatestFailures.
func TestHelperExit4(t *testing.T) {
	if os.Getenv("NIBFIN_TEST_EXIT4") == "1" {
		os.Exit(4)
	}
}
