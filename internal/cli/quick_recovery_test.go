package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type quickFailureHost struct {
	check     func(string) error
	configure func(string) error
	calls     int
}

func (f *quickFailureHost) Check(path string) error {
	if f.check != nil {
		return f.check(path)
	}
	return nil
}

func (f *quickFailureHost) Configure(path, _ string, _ map[string]string) error {
	f.calls++
	return f.configure(path)
}

func TestQuickConfigurationFailureCannotBeRetriedOrCancelledAway(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	digest := f.issue(t)
	host := &quickFailureHost{configure: func(string) error {
		return errors.New("synthetic configuration failure " + runnerTokenSentinel)
	}}
	f.app.Runner = host
	f.register(t, digest)
	for _, operation := range []string{"inspect", "cancel"} {
		if err := f.run(operation, true); err == nil ||
			!strings.Contains(f.output.String(), "pending") {
			t.Fatal("unknown configuration must remain Pending")
		}
	}
	if err := f.run("register", true, "--secret-file", f.secret,
		"--trusted-digest", digest); err == nil || host.calls != 1 {
		t.Fatal("unknown configuration was repeated")
	}
	state, err := loadQuick(f.member)
	if err != nil || state.Stage != "registration-pending" {
		t.Fatal("unknown registration lost recovery state")
	}
}

func TestQuickCancelledCapabilityIsRetiredWithoutRevocationClaim(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	f.issue(t)
	f.api.member = true
	if err := f.run("cancel", true, "--secret-file", f.secret); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(f.secret); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancel did not retire temporary capability")
	}
	if f.host.calls != 0 || !strings.Contains(f.output.String(), "remain valid") {
		t.Fatal("cancellation configured a Runner or claimed provider revocation")
	}
}

func TestQuickDownloadExpiryAndPersistenceFailuresStayPending(t *testing.T) {
	for _, failure := range []string{"expiry", "package", "runner-state"} {
		t.Run(failure, func(t *testing.T) {
			f := newQuickFixture(t)
			f.request(t)
			digest := f.issue(t)
			download := f.app.Download
			f.app.Download = func(app RunnerApplication, path string) error {
				if failure == "expiry" {
					f.api.base.now = f.api.base.now.Add(time.Hour)
				}
				if failure == "package" {
					return os.WriteFile(path, []byte("forged package"), 0600)
				}
				return download(app, path)
			}
			host := &quickFailureHost{}
			if failure == "runner-state" {
				host.check = func(path string) error {
					path = filepath.Join(path, runnerRecordName)
					if err := os.Remove(path); err != nil {
						return err
					}
					return os.Mkdir(path, 0700)
				}
			}
			f.app.Runner = host
			f.register(t, digest)
			if host.calls != 0 {
				t.Fatal("expired token, forged package or persistence failure configured")
			}
			state, err := loadQuick(f.member)
			if err != nil || state.Stage != "registration-pending" {
				t.Fatal("failure did not preserve nonsecret recovery identity")
			}
		})
	}
}

func TestQuickUnavailableManagementReadbackRecoversByObservation(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	f.register(t, f.issue(t))
	f.api.member, f.api.denyRead = false, true
	if err := f.run("confirm", true, "--result", filepath.Join(f.member, "result.json"),
		"--confirmation", f.proof); err == nil ||
		!strings.Contains(f.output.String(), "pending") {
		t.Fatal("unavailable provider confirmation must remain Pending")
	}
	f.api.denyRead = false
	digest := f.confirm(t)
	f.api.member = true
	if err := f.run("reconcile", true, "--confirmation", f.proof,
		"--trusted-digest", digest); err != nil {
		t.Fatal(err)
	}
	if f.api.base.tokens != 1 || f.host.calls != 1 {
		t.Fatal("readback recovery repeated registration")
	}
}

func TestQuickCompletionPersistenceFailureRetainsPendingRunner(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	f.register(t, f.issue(t))
	digest := f.confirm(t)
	f.api.member = true
	path := filepath.Join(f.runner, runnerRecordName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.run("reconcile", true, "--confirmation", f.proof,
		"--trusted-digest", digest); err == nil ||
		strings.Contains(f.output.String(), "runner_registered") {
		t.Fatal("unpersisted completion must not report registration success")
	}
	state, err := loadQuick(f.member)
	if err != nil || state.Stage != "registration-pending" ||
		len(f.api.base.runners) != 1 || f.host.calls != 1 {
		t.Fatal("failed completion removed or replaced an unresolved registration")
	}
}

func TestQuickJournalWriteFailureNeverDestroysOriginalEvidence(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	state, err := loadQuick(f.member)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(f.member, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if os.Mkdir(filepath.Join(f.member, "state-0001.json"), 0700) != nil {
		t.Fatal("fixture journal failure setup")
	}
	state.Stage = "registration-pending"
	if err := saveQuick(f.member, state, false); err == nil {
		t.Fatal("journal should reject a missing predecessor record")
	}
	current, err := os.ReadFile(filepath.Join(f.member, "state.json"))
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("failed persistence destroyed original recovery evidence")
	}
}

func TestQuickPrivateBoundedInputAndStrictDecode(t *testing.T) {
	for _, data := range []string{`{} {}`, `{"unknown":true}`, `null`, `[]`} {
		var request quickRequest
		if err := decodeQuick([]byte(data), &request); err == nil &&
			validQuickRequest(request) {
			t.Fatal("malformed request accepted")
		}
	}
	if runtime.GOOS == "windows" {
		return // Native Windows ACL contracts run in required Windows CI.
	}
	root := privateRunnerTestRoot(t)
	path := filepath.Join(root, "input.json")
	if os.WriteFile(path, []byte("{}"), 0644) != nil {
		t.Fatal("fixture input write failed")
	}
	if _, err := readQuick(path, true); err == nil {
		t.Fatal("public-readable capability accepted")
	}
	link := filepath.Join(root, "symlink.json")
	if os.Symlink(path, link) != nil {
		t.Fatal("fixture symlink failed")
	}
	if _, err := readQuick(link, true); err == nil {
		t.Fatal("symlink capability accepted")
	}
	if _, err := quickFilePath(filepath.Join("/work", "secret.json")); err == nil {
		t.Fatal("capability inside checkout accepted")
	}
}

func TestQuickOnlineBusyNeverChangesRecipient(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	f.register(t, f.issue(t))
	f.api.base.runners[0].Status, f.api.base.runners[0].Busy = "online", true
	f.confirm(t)
	if f.api.base.runners[0].Labels[0].Name != memberLabel(8) {
		t.Fatal("Online/Busy status changed member selection")
	}
}

type quickIssuanceFault struct {
	api   API
	fault func()
}

func (f quickIssuanceFault) Call(method, path string, input, output any) error {
	err := f.api.Call(method, path, input, output)
	if err == nil && method == "POST" && strings.HasSuffix(path, "/registration-token") {
		f.fault()
	}
	return err
}

type quickOutputFailure struct{ writes int }

func (f *quickOutputFailure) Write(data []byte) (int, error) {
	f.writes++
	if f.writes == 1 {
		return len(data), nil // Allow the preview, then lose the issuance output.
	}
	return 0, errors.New("synthetic output unavailable")
}

func TestQuickIssuanceAbortRetiresCapabilityAndCannotReissue(t *testing.T) {
	for _, failure := range []string{"persistence", "output"} {
		t.Run(failure, func(t *testing.T) {
			f := newQuickFixture(t)
			f.request(t)
			f.api.member = false
			if failure == "persistence" {
				f.app.API = quickIssuanceFault{api: f.api, fault: func() {
					path := filepath.Join(f.manager, "state-0001.json")
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal("fixture issuance failure setup")
					}
				}}
			}
			args := []string{"init", "--quick", "issue", "--repo", "example/project",
				"--handoff-directory", f.manager, "--member", "member",
				"--request", filepath.Join(f.member, "request.json"),
				"--secret-file", f.secret, "--apply"}
			var output bytes.Buffer
			var err error
			if failure == "output" {
				err = f.app.Run(args, &quickOutputFailure{})
			} else {
				err = f.app.Run(args, &output)
			}
			if err == nil || f.api.base.tokens != 1 || f.host.calls != 0 {
				t.Fatal("issuance fault did not preserve the external-effect boundary")
			}
			if _, err := os.Lstat(f.secret); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("aborted issuance retained a temporary bearer capability")
			}
			if err := f.app.Run(args, &output); err == nil || f.api.base.tokens != 1 {
				t.Fatal("aborted issuance requested another token automatically")
			}
			if _, err := os.Stat(filepath.Join(f.manager, "state.json")); err != nil {
				t.Fatal("issuance abort lost its nonsecret recovery identity")
			}
		})
	}
}

func TestQuickProviderExpiryIsNeverExtended(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	f.api.member, f.api.base.expired = false, true
	err := f.run("issue", true, "--request", filepath.Join(f.member, "request.json"),
		"--member", "member", "--secret-file", f.secret)
	if err == nil || !strings.Contains(f.output.String(), "pending") ||
		f.api.base.tokens != 1 || f.host.calls != 0 {
		t.Fatal("expired provider capability was not rejected")
	}
	if _, err := os.Lstat(f.secret); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired provider capability was disclosed")
	}
	state, err := loadQuick(f.manager)
	if err != nil || state.Stage != "issuance-pending" || state.Expires != "" {
		t.Fatal("issuance extended the provider expiry or lost its recovery intent")
	}
}

func TestQuickIssuanceSerializesManagementConfirmation(t *testing.T) {
	f := newQuickFixture(t)
	f.request(t)
	f.api.member = false
	attempted := false
	f.app.API = quickIssuanceFault{api: f.api, fault: func() {
		attempted = true
		err := f.run("confirm", true, "--result", filepath.Join(f.member, "result.json"),
			"--confirmation", f.proof)
		var command *CommandError
		if !errors.As(err, &command) || command.Exit != 2 ||
			command.Code != "HANDOFF_LOCKED_INSPECT_BEFORE_RETRY" {
			t.Fatal("confirmation raced with active management issuance")
		}
	}}
	if err := f.run("issue", true, "--request", filepath.Join(f.member, "request.json"),
		"--member", "member", "--secret-file", f.secret); err != nil {
		t.Fatal(err)
	}
	if !attempted || f.api.base.tokens != 1 {
		t.Fatal("issuance interleaving was not exercised")
	}
	state, err := loadQuick(f.manager)
	if err != nil || state.Stage != "issued" {
		t.Fatal("concurrent observation corrupted the management state")
	}
}
