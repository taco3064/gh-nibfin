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

func TestQuickUnknownConfigurationReconcilesWithoutRetry(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	digest := q.issue(t)
	q.host.lost = true // Official host effect occurred, but its result was lost.
	requireQuickPending(t, q.receive(t, digest))
	requireQuickPending(t, q.receive(t, digest))
	if q.host.calls != 1 || q.provider.tokens != 1 {
		t.Fatal("unknown effect retried")
	}
	q.management.fail = "repos/example/project/actions/runners?per_page=100&page=1"
	_, err := q.run("confirm", true)
	requireQuickPending(t, err)
	_, statErr := os.Stat(filepath.Join(q.managerDir, quickConfirmationFile))
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("failed read-back produced confirmation")
	}
	q.management.fail = ""
	proof := q.confirm(t)
	if _, err := q.run("finish", true, "--confirmation",
		filepath.Join(q.managerDir, quickConfirmationFile), "--approval", proof); err != nil {
		t.Fatal(err)
	}
	if q.host.calls != 1 || q.provider.tokens != 1 {
		t.Fatal("reconciliation configured")
	}
}

func TestQuickReadbackAndLocalSettingsFailClosed(t *testing.T) {
	for _, kind := range []string{"missing", "duplicate", "wrong-label", "extra-label",
		"local-repository", "local-id", "local-name", "member-revoked", "proof-origin",
		"proof-attempt", "online-busy"} {
		t.Run(kind, func(t *testing.T) {
			q := newQuickFixture(t)
			q.request(t)
			digest := q.issue(t)
			requireQuickPending(t, q.receive(t, digest))
			switch kind {
			case "missing":
				q.provider.runners = nil
			case "duplicate":
				q.provider.runners = append(q.provider.runners, q.provider.runners[0])
			case "wrong-label":
				q.provider.runners[0].Labels[0].Name = memberLabel(9)
			case "extra-label":
				label := q.provider.runners[0].Labels[0]
				label.Name = memberLabel(9)
				q.provider.runners[0].Labels = append(q.provider.runners[0].Labels, label)
			case "member-revoked":
				q.management.permission = "read"
			case "online-busy":
				q.provider.runners[0].Busy, q.provider.runners[0].Status = true, "online"
			}
			_, err := q.run("confirm", true)
			if kind == "missing" || kind == "duplicate" || kind == "wrong-label" ||
				kind == "extra-label" || kind == "member-revoked" {
				if err == nil {
					t.Fatal("unconfirmed registration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			proofPath := filepath.Join(q.managerDir, quickConfirmationFile)
			data, _ := os.ReadFile(proofPath)
			proof := quickDigest(data)
			switch kind {
			case "local-repository", "local-id", "local-name":
				settings := map[string]any{"agentId": 42, "agentName": "team-runner",
					"gitHubUrl": "https://github.com/example/project"}
				if kind == "local-repository" {
					settings["gitHubUrl"] = "https://github.com/evil/repo"
				}
				if kind == "local-id" {
					settings["agentId"] = 99
				}
				if kind == "local-name" {
					settings["agentName"] = "someone-else"
				}
				if err := quickWrite(filepath.Join(q.runnerDir, ".runner"), settings); err != nil {
					t.Fatal(err)
				}
			case "proof-origin":
				proof = strings.Repeat("f", 64)
			case "proof-attempt":
				var confirmation quickConfirmation
				if _, err := quickRead(proofPath, &confirmation); err != nil {
					t.Fatal(err)
				}
				confirmation.Request.Attempt = strings.Repeat("b", 64)
				if err := quickWrite(proofPath, confirmation); err != nil {
					t.Fatal(err)
				}
				data, _ = os.ReadFile(proofPath)
				proof = quickDigest(data)
			}
			_, err = q.run("finish", true, "--confirmation", proofPath, "--approval", proof)
			if kind == "online-busy" {
				if err != nil {
					t.Fatal("busy status must not select another member")
				}
			} else if err == nil {
				t.Fatal("untrusted proof or stale settings accepted")
			}
		})
	}
}

type quickHostAction struct {
	base      RunnerHost
	check     func(string) error
	configure func(string) error
}

func (h quickHostAction) Check(path string) error {
	if h.check != nil {
		return h.check(path)
	}
	return h.base.Check(path)
}

func (h quickHostAction) Configure(path, command string, inputs map[string]string) error {
	if h.configure != nil {
		return h.configure(path)
	}
	return h.base.Configure(path, command, inputs)
}

func TestQuickConfigurationFailureInterruptionAndPersistence(t *testing.T) {
	for _, kind := range []string{"configuration-failure", "interrupted", "state-write",
		"slow-download", "bad-package", "private-parent"} {
		t.Run(kind, func(t *testing.T) {
			q := newQuickFixture(t)
			q.request(t)
			digest := q.issue(t)
			switch kind {
			case "configuration-failure", "interrupted":
				q.app.Runner = quickHostAction{base: q.host, configure: func(string) error {
					return errors.New("SYNTHETIC_LOST_CONFIGURATION_RESULT")
				}}
			case "state-write":
				// The host preflight seam injects a deterministic persistence failure
				// before configuration. This also works for root and on native Windows.
				q.app.Runner = quickHostAction{base: q.host, check: func(path string) error {
					record := filepath.Join(path, runnerRecordName)
					if err := os.Remove(record); err != nil {
						return err
					}
					return os.Mkdir(record, 0700)
				}}
			case "slow-download":
				download := q.app.Download
				q.app.Download = func(app RunnerApplication, path string) error {
					q.provider.now = q.provider.now.Add(time.Hour)
					return download(app, path)
				}
			case "bad-package":
				q.app.Download = func(_ RunnerApplication, path string) error {
					return os.WriteFile(path, []byte("tampered archive"), 0600)
				}
			case "private-parent":
				if runtime.GOOS == "windows" {
					t.Skip("Unix mode rejection; native Windows ACL checks are separate")
				}
				if err := os.Chmod(q.root, 0777); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(q.root, 0700) })
			}
			err := q.receive(t, digest)
			if err == nil || q.host.calls != 0 {
				t.Fatal("unsafe configuration accepted")
			}
			// A pre-configuration safety rejection may leave awaiting; an unknown
			// effect must remain Pending. Neither path automatically issues a token.
			if kind != "private-parent" {
				requireQuickPending(t, q.receive(t, digest))
			}
			if q.provider.tokens != 1 {
				t.Fatal("failure automatically requested a new token")
			}
		})
	}
}

func TestQuickUnknownIssuanceNeverRetries(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	q.management.fail = "repos/example/project/actions/runners/registration-token"
	args := []string{"--request", filepath.Join(q.memberDir, quickRequestFile)}
	_, err := q.run("issue", true, args...)
	requireQuickPending(t, err)
	q.management.fail = ""
	if _, err := q.run("issue", true, args...); err == nil {
		t.Fatal("unknown issuance retried")
	}
	if q.provider.tokens != 0 {
		t.Fatal("retry issued replacement token")
	}
}

func TestQuickReceivePreviewDoesNotConsumeCapability(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	digest := q.issue(t)
	path := filepath.Join(q.managerDir, quickHandoffFile)
	before, _ := os.ReadFile(path)
	output, err := q.run("receive", false, "--handoff", path, "--approval", digest)
	after, _ := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || q.host.calls != 0 ||
		strings.Contains(output, runnerTokenSentinel) {
		t.Fatal("preview consumed or exposed the handoff")
	}
}

func TestQuickExpiredManagementObservationCannotComplete(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	digest := q.issue(t)
	requireQuickPending(t, q.receive(t, digest))
	proof := q.confirm(t)
	q.provider.now = q.provider.now.Add(6 * time.Minute)
	_, err := q.run("finish", true, "--confirmation",
		filepath.Join(q.managerDir, quickConfirmationFile), "--approval", proof)
	if err == nil {
		t.Fatal("stale management observation must not confirm registration")
	}
	proof = q.confirm(t)
	if _, err := q.run("finish", true, "--confirmation",
		filepath.Join(q.managerDir, quickConfirmationFile), "--approval", proof); err != nil {
		t.Fatal("fresh independent read-back should permit reconciliation")
	}
}

func TestQuickRecipientIdentityAndRepositoryChangesFailBeforeConfiguration(t *testing.T) {
	for _, kind := range []string{"member", "repository"} {
		t.Run(kind, func(t *testing.T) {
			q := newQuickFixture(t)
			q.request(t)
			digest := q.issue(t)
			if kind == "member" {
				q.member.member.ID++
			} else {
				repo := q.provider.data["repos/example/project"].(Repository)
				repo.ID++
				q.provider.data["repos/example/project"] = repo
			}
			if err := q.receive(t, digest); err == nil || q.host.calls != 0 {
				t.Fatal("changed authenticated identity or repository configured a Runner")
			}
		})
	}
}

type quickLostOutput struct{ writes int }

func (out *quickLostOutput) Write(data []byte) (int, error) {
	out.writes++
	if out.writes > 1 {
		return 0, errors.New("SYNTHETIC_OUTPUT_LOST")
	}
	return len(data), nil
}

func TestQuickLostIssuanceOutputPreservesPendingAttempt(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	app := q.app
	app.API = q.management
	args := []string{"init", "--repo", "example/project", "--quick-runner", "issue",
		"--quick-directory", q.managerDir, "--request",
		filepath.Join(q.memberDir, quickRequestFile), "--apply"}
	requireQuickPending(t, app.Run(args, &quickLostOutput{}))
	if q.provider.tokens != 1 {
		t.Fatal("issuance effect was not exercised")
	}
	if err := app.Run(args, &bytes.Buffer{}); err == nil || q.provider.tokens != 1 {
		t.Fatal("lost output must not cause replacement issuance")
	}
	var state quickState
	_, err := quickRead(filepath.Join(q.managerDir, quickStateFile), &state)
	if err != nil || state.Phase != "issued-pending" {
		t.Fatal("lost output discarded the nonsecret recovery journal")
	}
}

func TestQuickTamperedLocalDirectoryCannotBypassRunnerProtections(t *testing.T) {
	q := newQuickFixture(t)
	q.request(t)
	digest := q.issue(t)
	path := filepath.Join(q.memberDir, quickStateFile)
	var state quickState
	if _, err := quickRead(path, &state); err != nil {
		t.Fatal(err)
	}
	state.Directory = "relative-runner"
	if err := quickWrite(path, state); err != nil {
		t.Fatal(err)
	}
	if err := q.receive(t, digest); err == nil || q.host.calls != 0 {
		t.Fatal("local observations must not bypass private Runner directory validation")
	}
}
