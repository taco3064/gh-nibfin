package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Synthetic provider/host contracts only; these never register with GitHub.
type quickTestAPI struct {
	managerType     string
	base            *runnerAPI
	member          User
	management      bool
	fail            string
	permission      string
	memberReads     int
	managementReads int
}

func (f *quickTestAPI) Call(method, path string, input, output any) error {
	if path == f.fail {
		return errors.New("SYNTHETIC_PROVIDER_UNAVAILABLE")
	}
	if path == "user" {
		if f.management {
			kind := f.managerType
			if kind == "" {
				kind = "User"
			}
			return assign(output, User{ID: 7, Login: "operator", Type: kind})
		}
		f.memberReads++
		return assign(output, f.member)
	}
	if path == "repos/example/project" {
		repo := f.base.data[path].(Repository)
		repo.Permissions = map[string]bool{"push": true, "admin": f.management}
		return assign(output, repo)
	}
	if path == "users/teammate" {
		return assign(output, f.member)
	}
	if path == "repos/example/project/collaborators/teammate/permission" {
		return assign(output, map[string]any{"permission": f.permission, "user": f.member})
	}
	if strings.Contains(path, "/actions/runners") {
		if !f.management {
			return errors.New("MEMBER_MUST_NOT_USE_MANAGEMENT_ENDPOINT")
		}
		f.managementReads++
	}
	return f.base.Call(method, path, input, output)
}

type quickFixture struct {
	app                                    App
	provider                               *runnerAPI
	host                                   *runnerHostFixture
	management, member                     *quickTestAPI
	root, managerDir, memberDir, runnerDir string
}

func newQuickFixture(t *testing.T) *quickFixture {
	t.Helper()
	app, api, host, runner := runnerFixture(t)
	root := filepath.Dir(runner)
	member := User{ID: 8, Login: "teammate", Type: "User"}
	q := &quickFixture{app: app, provider: api, host: host, root: root,
		runnerDir: runner, managerDir: filepath.Join(root, "management-attempt"),
		memberDir: filepath.Join(root, "member-attempt")}
	q.management = &quickTestAPI{base: api, management: true, member: member,
		permission: "write"}
	q.member = &quickTestAPI{base: api, member: member, permission: "write"}
	return q
}

func (q *quickFixture) run(command string, apply bool, extra ...string) (string, error) {
	directory, api := q.memberDir, q.member
	if command == "issue" || command == "confirm" {
		directory, api = q.managerDir, q.management
	}
	args := []string{"init", "--quick-runner", command, "--repo", "example/project",
		"--quick-directory", directory}
	args = append(args, extra...)
	if apply {
		args = append(args, "--apply")
	}
	app := q.app
	app.API = api
	var output bytes.Buffer
	err := app.Run(args, &output)
	return output.String(), err
}

func (q *quickFixture) request(t *testing.T) {
	t.Helper()
	_, err := q.run("request", true, "--runner-directory", q.runnerDir,
		"--runner-name", "team-runner")
	if err != nil {
		t.Fatal(err)
	}
}

func (q *quickFixture) issue(t *testing.T) string {
	t.Helper()
	_, err := q.run("issue", true, "--request",
		filepath.Join(q.memberDir, quickRequestFile))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(q.managerDir, quickHandoffFile))
	if err != nil {
		t.Fatal("missing private handoff")
	}
	return quickDigest(data)
}

func (q *quickFixture) receive(t *testing.T, approval string) error {
	t.Helper()
	_, err := q.run("receive", true, "--handoff",
		filepath.Join(q.managerDir, quickHandoffFile), "--approval", approval)
	return err
}

func (q *quickFixture) confirm(t *testing.T) string {
	t.Helper()
	_, err := q.run("confirm", true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(q.managerDir, quickConfirmationFile))
	if err != nil {
		t.Fatal(err)
	}
	return quickDigest(data)
}

func requireQuickPending(t *testing.T, err error) {
	t.Helper()
	var result *CommandError
	if !errors.As(err, &result) || result.Exit != 2 {
		t.Fatalf("expected Pending without retry, got %v", err)
	}
}

func TestQuickSeparatedPersonalAndOrganizationRegistration(t *testing.T) {
	for _, owner := range []string{"User", "Organization"} {
		t.Run(owner, func(t *testing.T) {
			q := newQuickFixture(t)
			repo := q.provider.data["repos/example/project"].(Repository)
			repo.Owner.Type = owner
			q.provider.data["repos/example/project"] = repo
			q.request(t)
			digest := q.issue(t)
			requireQuickPending(t, q.receive(t, digest))
			if q.host.calls != 1 || !q.host.tokenPresent || q.provider.tokens != 1 {
				t.Fatal("must configure once using only the short-lived capability")
			}
			if q.member.managementReads != 0 || q.member.memberReads < 2 {
				t.Fatal("member must authenticate without management API access")
			}
			proof := q.confirm(t)
			output, err := q.run("finish", true, "--confirmation",
				filepath.Join(q.managerDir, quickConfirmationFile), "--approval", proof)
			if err != nil || !strings.Contains(output, `"status":"runner_registered"`) {
				t.Fatalf("management read-back plus actual settings must confirm: %v", err)
			}
			if !strings.Contains(output, `"core_binding":"unverified"`) ||
				!strings.Contains(output, `"host_adoption":"required"`) ||
				strings.Contains(output, runnerTokenSentinel) {
				t.Fatal("registration must neither disclose secrets nor grant execution")
			}
			for _, path := range []string{filepath.Join(q.memberDir, quickStateFile),
				filepath.Join(q.managerDir, quickStateFile),
				filepath.Join(q.runnerDir, runnerRecordName)} {
				data, err := os.ReadFile(path)
				if err != nil || bytes.Contains(data, []byte(runnerTokenSentinel)) {
					t.Fatal("ordinary records must retain only nonsecret recovery context")
				}
			}
			requireQuickPending(t, q.receive(t, digest))
			if q.host.calls != 1 {
				t.Fatal("replay must not reconfigure")
			}
		})
	}
}

func TestQuickPreviewAndConflictingOptions(t *testing.T) {
	q := newQuickFixture(t)
	_, err := q.run("request", false, "--runner-directory", q.runnerDir,
		"--runner-name", "team-runner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(q.memberDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview must not persist an attempt")
	}
	q.request(t)
	_, err = q.run("issue", false, "--request", filepath.Join(q.memberDir, quickRequestFile))
	if err != nil {
		t.Fatal(err)
	}
	if q.provider.tokens != 0 || q.host.calls != 0 {
		t.Fatal("preview must not issue or configure")
	}
	for _, extra := range [][]string{
		{"--request", "anything", "--runner-name", "conflict"},
		{"--handoff", "anything", "--approval", "malformed"},
		{"--apply", "positional"}, {"--unknown"},
	} {
		if _, err := q.run("issue", false, extra...); err == nil {
			t.Fatal("malformed or conflicting options accepted")
		}
	}
	var output bytes.Buffer
	if err := q.app.Run([]string{"help"}, &output); err != nil ||
		!strings.Contains(output.String(), "--quick-runner") {
		t.Fatal("help must expose optional team path")
	}
}

func TestQuickMemberCannotIssueAndManagementDenials(t *testing.T) {
	for _, kind := range []string{"member", "token-permission", "eligibility",
		"forged-id", "repository-id", "owner-type", "app-user"} {
		t.Run(kind, func(t *testing.T) {
			q := newQuickFixture(t)
			q.request(t)
			switch kind {
			case "member":
				q.management.management = false
			case "token-permission":
				q.management.fail = "repos/example/project/actions/runners/registration-token"
			case "eligibility":
				q.management.permission = "read"
			case "forged-id":
				var request quickRequest
				path := filepath.Join(q.memberDir, quickRequestFile)
				if _, err := quickRead(path, &request); err != nil {
					t.Fatal(err)
				}
				request.Member.ID++
				if err := quickWrite(path, request); err != nil {
					t.Fatal(err)
				}
			case "repository-id", "owner-type":
				repo := q.provider.data["repos/example/project"].(Repository)
				if kind == "repository-id" {
					repo.ID++
				} else {
					repo.Owner.Type = "Bot"
				}
				q.provider.data["repos/example/project"] = repo
			case "app-user":
				q.management.managerType = "Bot"
			}
			requestPath := filepath.Join(q.memberDir, quickRequestFile)
			_, err := q.run("issue", true, "--request", requestPath)
			if err == nil || q.host.calls != 0 {
				t.Fatal("invalid authority must not configure")
			}
			if kind != "token-permission" && q.provider.tokens != 0 {
				t.Fatal("identity and repository checks must precede token issuance")
			}
		})
	}
}

func TestQuickReceiveRejectsTamperingAndRetiresSecrets(t *testing.T) {
	for _, kind := range []string{"origin", "member", "attempt", "repo", "expired",
		"malformed-expiry", "short-expiry", "oversized", "malformed", "source"} {
		t.Run(kind, func(t *testing.T) {
			q := newQuickFixture(t)
			q.request(t)
			digest := q.issue(t)
			path := filepath.Join(q.managerDir, quickHandoffFile)
			var handoff quickHandoff
			if _, err := quickRead(path, &handoff); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "origin":
				digest = strings.Repeat("0", 64)
			case "member":
				handoff.Request.Member.ID++
			case "attempt":
				handoff.Request.Attempt = strings.Repeat("b", 64)
			case "repo":
				handoff.Request.RepositoryID++
			case "expired":
				handoff.Expires = "2000-01-01T00:00:00Z"
			case "malformed-expiry":
				handoff.Expires = "tomorrow"
			case "short-expiry":
				handoff.Expires = q.provider.now.Add(time.Minute).Format(time.RFC3339)
			case "source":
				handoff.Application.URL = "https://example.com/runner.tar.gz"
			}
			if kind != "origin" {
				if err := quickWrite(path, handoff); err != nil {
					t.Fatal(err)
				}
				if kind == "oversized" || kind == "malformed" {
					data := []byte("invalid JSON")
					if kind == "oversized" {
						data = bytes.Repeat([]byte("x"), 32769)
					}
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				data, _ := os.ReadFile(path)
				digest = quickDigest(data)
			}
			if err := q.receive(t, digest); err == nil || q.host.calls != 0 {
				t.Fatal("invalid handoff must not configure even with an authentic token")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("consumed secret material must be retired even on rejection")
			}
		})
	}
}

func TestQuickCancellationAndNoAutomaticRetries(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	digest := q.issue(t)
	if _, err := q.run("abort", true); err != nil {
		t.Fatal(err)
	}
	requireQuickPending(t, q.receive(t, digest))
	if q.host.calls != 0 {
		t.Fatal("aborted attempt configured")
	}
	// Management cancellation retires its copy but cannot revoke a disclosed token.
	app := q.app
	app.API = q.management
	err := app.Run([]string{"init", "--quick-runner", "abort", "--repo", "example/project",
		"--quick-directory", q.managerDir, "--apply"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(filepath.Join(q.managerDir, quickHandoffFile))
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("management secret retained after abort")
	}
	if q.provider.tokens != 1 {
		t.Fatal("abort must not issue or claim provider revocation")
	}
}
