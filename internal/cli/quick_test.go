package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// All handoff tests are synthetic provider/host contracts, not live registration.
type quickAPI struct {
	base        *runnerAPI
	member      bool
	denyToken   bool
	denyRead    bool
	denyMember  bool
	memberID    int64
	repoID      int64
	owner       string
	downloads   int
	credentials int
}

func (f *quickAPI) Call(method, path string, input, output any) error {
	if path == "repos/example/project" {
		repo := f.base.fakeAPI.data[path].(Repository)
		repo.Permissions = map[string]bool{"push": true, "admin": !f.member}
		if f.denyMember && f.member {
			repo.Permissions["push"] = false
		}
		if f.repoID != 0 {
			repo.ID = f.repoID
		}
		if f.owner != "" {
			repo.Owner.Type = f.owner
		}
		return assign(output, repo)
	}
	if path == "user" && f.member {
		id := int64(8)
		if f.memberID != 0 {
			id = f.memberID
		}
		return assign(output, User{ID: id, Login: "member", Type: "User"})
	}
	if path == "users/member" {
		return assign(output, User{ID: 8, Login: "member", Type: "User"})
	}
	if path == "repos/example/project/collaborators/member/permission" {
		permission := "write"
		if f.denyMember {
			permission = "read"
		}
		return assign(output, map[string]any{"permission": permission,
			"user": User{ID: 8, Login: "member", Type: "User"}})
	}
	if strings.Contains(path, "/actions/runners") {
		if f.member {
			f.credentials++
			return errors.New("MEMBER_MUST_NOT_USE_MANAGEMENT_ENDPOINT")
		}
		if strings.HasSuffix(path, "/downloads") {
			f.downloads++
		}
		if method == "POST" && f.denyToken {
			return errors.New("PERMISSION_DENIED")
		}
		if method == "GET" && f.denyRead && !strings.HasSuffix(path, "/downloads") {
			return errors.New("PROVIDER_READBACK_UNAVAILABLE")
		}
	}
	return f.base.Call(method, path, input, output)
}

type quickFixture struct {
	app                   App
	api                   *quickAPI
	host                  *runnerHostFixture
	root, member, manager string
	runner, secret, proof string
	output                bytes.Buffer
}

func newQuickFixture(t *testing.T) *quickFixture {
	t.Helper()
	app, api, host, runner := runnerFixture(t)
	root := filepath.Dir(runner)
	provider := &quickAPI{base: api, member: true}
	app.API = provider
	return &quickFixture{app: app, api: provider, host: host, root: root,
		member: filepath.Join(root, "member-attempt"), runner: runner,
		manager: filepath.Join(root, "management-attempt"),
		secret:  filepath.Join(root, "capability.json"),
		proof:   filepath.Join(root, "confirmation.json")}
}

func (f *quickFixture) run(operation string, apply bool, options ...string) error {
	f.output.Reset()
	directory := f.member
	if operation == "issue" || operation == "confirm" {
		directory = f.manager
	}
	args := []string{"init", "--quick", operation, "--repo", "example/project",
		"--handoff-directory", directory}
	args = append(args, options...)
	if apply {
		args = append(args, "--apply")
	}
	err := f.app.Run(args, &f.output)
	if strings.Contains(f.output.String(), runnerTokenSentinel) {
		return errors.New("SECRET_DISCLOSED")
	}
	return err
}

func (f *quickFixture) request(t *testing.T) {
	t.Helper()
	f.api.member = true
	if err := f.run("request", true, "--runner-directory", f.runner,
		"--runner-name", "member-runner"); err != nil {
		t.Fatal(err)
	}
}

func (f *quickFixture) issue(t *testing.T) string {
	t.Helper()
	f.api.member = false
	if err := f.run("issue", true, "--request", filepath.Join(f.member, "request.json"),
		"--member", "member", "--secret-file", f.secret); err != nil {
		t.Fatal(err)
	}
	return fileQuickDigest(t, f.secret)
}

func (f *quickFixture) register(t *testing.T, digest string) {
	t.Helper()
	f.api.member = true
	err := f.run("register", true, "--secret-file", f.secret,
		"--trusted-digest", digest)
	var command *CommandError
	if !errors.As(err, &command) || command.Exit != 2 ||
		!strings.Contains(f.output.String(), "pending") {
		t.Fatalf("registration must await management confirmation: %v", err)
	}
	if _, err := os.Lstat(f.secret); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("registration capability was not retired")
	}
}

func (f *quickFixture) confirm(t *testing.T) string {
	t.Helper()
	f.api.member = false
	if err := f.run("confirm", true, "--result", filepath.Join(f.member, "result.json"),
		"--confirmation", f.proof); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.output.String(), "runner_registered") {
		t.Fatal("management observation cannot establish the local result")
	}
	return fileQuickDigest(t, f.proof)
}

func fileQuickDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("missing synthetic exchange file")
	}
	return quickDigest(data)
}

func TestQuickPersonalAndOrganizationSeparatedHandoff(t *testing.T) {
	for _, owner := range []string{"User", "Organization"} {
		t.Run(owner, func(t *testing.T) {
			f := newQuickFixture(t)
			f.api.owner = owner
			f.request(t)
			digest := f.issue(t)
			if f.api.base.tokens != 1 {
				t.Fatal("expected exactly one management token")
			}
			f.register(t, digest)
			if f.host.calls != 1 || !f.host.tokenPresent || f.api.credentials != 0 {
				t.Fatal("recipient must configure without management API credentials")
			}
			digest = f.confirm(t)
			f.api.member = true
			if err := f.run("reconcile", true, "--confirmation", f.proof,
				"--trusted-digest", digest); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"runner_registered", "unverified", "required"} {
				if !strings.Contains(f.output.String(), want) {
					t.Fatal("registration must retain the Core host-adoption boundary")
				}
			}
			state, err := loadQuick(f.member)
			if err != nil || state.Stage != "registered" || state.RunnerID != 42 {
				t.Fatal("local and native identities were not reconciled")
			}
			if err := f.run("register", true, "--secret-file", f.secret,
				"--trusted-digest", strings.Repeat("a", 64)); err == nil {
				t.Fatal("replay must never configure another Runner")
			}
			if f.host.calls != 1 || f.api.base.tokens != 1 {
				t.Fatal("replay repeated an external effect")
			}
			assertQuickNonsecretRecords(t, f)
		})
	}
}

func assertQuickNonsecretRecords(t *testing.T, f *quickFixture) {
	t.Helper()
	for _, root := range []string{f.member, f.manager, f.runner} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if bytes.Contains(data, []byte(runnerTokenSentinel)) {
				t.Error("secret persisted in an ordinary record")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestQuickPreviewsAndMalformedOptions(t *testing.T) {
	f := newQuickFixture(t)
	if err := f.run("request", false, "--runner-directory", f.runner,
		"--runner-name", "preview"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.member); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview created state")
	}
	f.request(t)
	f.api.member = false
	if err := f.run("issue", false, "--request", filepath.Join(f.member, "request.json"),
		"--member", "member", "--secret-file", f.secret); err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]string{
		{"--runner-directory", f.runner, "--runner-name", "bad name"},
		{"--runner-directory", "relative", "--runner-name", "name"},
		{"--runner-directory", f.runner, "--runner-name", "name", "--member", "member"},
		{"--runner-directory", f.runner, "--runner-name", "name", "--member", ""},
	} {
		if err := f.run("request", false, options...); err == nil {
			t.Fatal("malformed or conflicting options accepted")
		}
	}
	if err := f.run("register", false, "--secret-file", f.secret); err == nil {
		t.Fatal("unconfirmed management origin accepted")
	}
	if err := f.run("unknown", true); err == nil {
		t.Fatal("unknown operation accepted")
	}
	if f.api.base.tokens != 0 || f.host.calls != 0 {
		t.Fatal("preview changed the provider or host")
	}
}

func TestQuickManagementDenialsBeforeConfiguration(t *testing.T) {
	for _, denial := range []string{"member", "api-permission", "wrong-repository",
		"wrong-member", "forged-member", "revoked", "unsupported-owner", "app"} {
		t.Run(denial, func(t *testing.T) {
			f := newQuickFixture(t)
			f.request(t)
			f.api.member = false
			member := "member"
			switch denial {
			case "member":
				f.api.member = true
			case "api-permission":
				f.api.denyToken = true
			case "wrong-repository":
				f.api.repoID = 11
			case "wrong-member":
				member = "operator"
			case "forged-member":
				mutateQuickRequest(t, f, func(r *quickRequest) { r.MemberID = 99 })
			case "revoked":
				f.api.denyMember = true
			case "unsupported-owner":
				f.api.owner = "Enterprise"
			case "app":
				f.api.base.fakeAPI.data["user"] = User{ID: 9, Login: "app", Type: "Bot"}
			}
			err := f.run("issue", true, "--request", filepath.Join(f.member, "request.json"),
				"--member", member, "--secret-file", f.secret)
			if err == nil || f.host.calls != 0 || f.api.base.tokens != 0 {
				t.Fatal("management denial allowed token issuance or configuration")
			}
		})
	}
}

func mutateQuickRequest(t *testing.T, f *quickFixture, change func(*quickRequest)) {
	t.Helper()
	path := filepath.Join(f.member, "request.json")
	data, err := readQuick(path, false)
	var request quickRequest
	if err != nil || decodeQuick(data, &request) != nil {
		t.Fatal("missing request fixture")
	}
	change(&request)
	if _, err := writeQuick(path, request, false); err != nil {
		t.Fatal(err)
	}
}

func TestQuickRecipientRejectsAuthenticTokenWithWrongBinding(t *testing.T) {
	for _, denial := range []string{"member", "repository", "attempt", "origin",
		"expired", "malformed-expiry", "short-expiry", "malformed", "oversized",
		"wrong-package", "revoked", "cancelled"} {
		t.Run(denial, func(t *testing.T) {
			f := newQuickFixture(t)
			f.request(t)
			digest := f.issue(t)
			data, err := readQuick(f.secret, false)
			var capability quickCapability
			if err != nil || decodeQuick(data, &capability) != nil {
				t.Fatal("missing capability fixture")
			}
			f.api.member = true
			switch denial {
			case "member":
				f.api.memberID = 99
			case "repository":
				f.api.repoID = 11
			case "attempt":
				capability.Request.Attempt = strings.Repeat("b", 64)
			case "origin":
				digest = strings.Repeat("a", 64)
			case "expired":
				capability.Token.Expires = f.api.base.now.Add(-time.Second).Format(time.RFC3339)
			case "malformed-expiry":
				capability.Token.Expires = "tomorrow"
			case "short-expiry":
				capability.Token.Expires = f.api.base.now.Add(time.Minute).Format(time.RFC3339)
			case "wrong-package":
				capability.App.URL = "https://untrusted.invalid/runner"
			case "revoked":
				f.api.denyMember = true
			case "cancelled":
				if err := f.run("cancel", true); err != nil {
					t.Fatal(err)
				}
			}
			if denial != "origin" {
				var err error
				digest, err = writeQuick(f.secret, capability, false)
				if err != nil {
					t.Fatal(err)
				}
			}
			if denial == "malformed" || denial == "oversized" {
				data := []byte("invalid-json")
				if denial == "oversized" {
					data = bytes.Repeat([]byte("x"), 64*1024+1)
				}
				if os.WriteFile(f.secret, data, 0600) != nil {
					t.Fatal("fixture write failed")
				}
				digest = quickDigest(data)
			}
			err = f.run("register", true, "--secret-file", f.secret,
				"--trusted-digest", digest)
			if err == nil || f.host.calls != 0 || f.api.base.tokens != 1 {
				t.Fatal("invalid binding, expiry or package reached configuration")
			}
		})
	}
}

func TestQuickUnknownConfigurationRecoversWithoutReplacement(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	digest := f.issue(t)
	f.host.lost = true
	f.register(t, digest)
	if _, err := os.Stat(filepath.Join(f.member, "result.json")); err == nil {
		t.Fatal("timeout must require explicit inspection")
	}
	if err := f.run("inspect", true); err == nil {
		t.Fatal("inspection must still await management")
	}
	digest = f.confirm(t)
	f.api.member = true
	if err := f.run("reconcile", true, "--confirmation", f.proof,
		"--trusted-digest", digest); err != nil {
		t.Fatal(err)
	}
	if f.host.calls != 1 || f.api.base.tokens != 1 {
		t.Fatal("recovery must observe, never repeat registration")
	}
}

func TestQuickNativeAndLocalResultNegativeControls(t *testing.T) {
	for _, denial := range []string{"missing", "duplicate", "wrong-id", "label",
		"extra-label", "readback", "revoked", "stale-local", "wrong-local-repo",
		"wrong-confirmation", "unconfirmed-origin"} {
		t.Run(denial, func(t *testing.T) {
			f := newQuickFixture(t)
			f.request(t)
			f.register(t, f.issue(t))
			f.api.member = false
			switch denial {
			case "missing":
				f.api.base.runners = nil
			case "duplicate":
				f.api.base.runners = append(f.api.base.runners, f.api.base.runners[0])
			case "wrong-id":
				f.api.base.runners[0].ID = 99
			case "label":
				f.api.base.runners[0].Labels[0].Name = memberLabel(99)
			case "extra-label":
				label := f.api.base.runners[0].Labels[0]
				label.Name = memberLabel(99)
				f.api.base.runners[0].Labels = append(f.api.base.runners[0].Labels, label)
			case "readback":
				f.api.denyRead = true
			case "revoked":
				f.api.denyMember = true
			}
			err := f.run("confirm", true, "--result", filepath.Join(f.member, "result.json"),
				"--confirmation", f.proof)
			localDenial := denial == "stale-local" || denial == "wrong-local-repo" ||
				denial == "wrong-confirmation" || denial == "unconfirmed-origin"
			if !localDenial {
				if err == nil || strings.Contains(f.output.String(), "runner_registered") {
					t.Fatal("invalid native observation reported success")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			digest := fileQuickDigest(t, f.proof)
			if denial == "stale-local" || denial == "wrong-local-repo" {
				state, err := loadQuick(f.member)
				if err != nil {
					t.Fatal(err)
				}
				settings := map[string]any{"agentId": 42, "agentName": state.Request.Name,
					"gitHubUrl": "https://github.com/example/project"}
				if denial == "stale-local" {
					settings["agentId"] = 99
				} else {
					settings["gitHubUrl"] = "https://github.com/wrong/repository"
				}
				data, _ := json.Marshal(settings)
				if os.WriteFile(filepath.Join(f.runner, ".runner"), data, 0600) != nil {
					t.Fatal("fixture settings write failed")
				}
			} else if denial == "wrong-confirmation" {
				state, err := loadQuick(f.member)
				if err != nil {
					t.Fatal(err)
				}
				result := quickResult{Kind: "management-observation-v1",
					Request: state.Request, RunnerID: 99}
				digest, err = writeQuick(f.proof, result, false)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				digest = strings.Repeat("a", 64)
			}
			f.api.member = true
			if err := f.run("reconcile", true, "--confirmation", f.proof,
				"--trusted-digest", digest); err == nil {
				t.Fatal("local settings or untrusted confirmation reported success")
			}
			if f.host.calls != 1 {
				t.Fatal("reconciliation reconfigured a Runner")
			}
		})
	}
}

func TestQuickInterruptedStateRetainsMinimumRecoveryEvidence(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	digest := f.issue(t)
	// Simulate a failed new journal write, without destroying the original.
	path := filepath.Join(f.member, "state-0001.json")
	if os.WriteFile(path, []byte("{"), 0600) != nil {
		t.Fatal("fixture journal write failed")
	}
	f.api.member = true
	if err := f.run("register", true, "--secret-file", f.secret,
		"--trusted-digest", digest); err == nil || f.host.calls != 0 {
		t.Fatal("incomplete latest state must never fall back to retryable state")
	}
	if _, err := os.Stat(filepath.Join(f.member, "state.json")); err != nil {
		t.Fatal("original recovery identity was removed")
	}
}

func TestQuickNoSecretInArgumentsOrRunnerEnvironment(t *testing.T) {
	env := runnerEnvironment(map[string]string{"TOKEN": runnerTokenSentinel,
		"UNATTENDED": "true"})
	for _, value := range env {
		if strings.HasPrefix(value, "GH_TOKEN=") || strings.HasPrefix(value, "GITHUB_TOKEN=") {
			t.Fatal("management credential inherited by recipient")
		}
	}
	if err := (App{}).Run([]string{"help"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}
