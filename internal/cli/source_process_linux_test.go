package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSourceNativeOwnerIdentityAndSurvivingConsumer(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	owner := currentSourceOwner()
	if !validSourceOwner(owner) {
		t.Fatal("native own-process identity unavailable")
	}
	// An event-controlled process stands in for a surviving verifier consumer.
	// No host installation, downloaded Core or claimed host termination is used.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	cmd := exec.Command("/bin/sh", "-c", "read value")
	cmd.Stdin = reader
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader.Close()
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	source.record.Phase = "released"
	source.record.Group = cmd.Process.Pid
	if err := source.save(); err != nil {
		t.Fatal(err)
	}
	result := retireCoreSource(source.parent, source.name, source.root,
		source.record, observeSourceUse)
	if result.Status != "deferred" || !strings.Contains(result.Reason, "group") {
		t.Fatal("surviving/reused group must prevent removal", result)
	}
	if _, err := writer.WriteString("complete\n"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	waited = true
	result = retireCoreSource(source.parent, source.name, source.root,
		source.record, observeSourceUse)
	if result.Status != "removed" {
		t.Fatal("observed consumer exit should allow retirement", result)
	}
}

func TestSourceNativeVerifierGroupIsRecorded(t *testing.T) {
	var group int
	data, err := (NativeProcess{}).runSourceVerifier("sh",
		[]string{"-c", "printf verified"}, func(pid int) error {
			group = pid
			return nil
		})
	if err != nil || group <= 0 || string(data) != "verified" {
		t.Fatal("native launch did not record its actual verifier group", group, err)
	}
	if err := syscall.Kill(-group, 0); err != syscall.ESRCH {
		t.Fatal("synchronous verifier group unexpectedly remains", err)
	}
}

func TestSourceAppliedUpgradeReconcilesWithoutExtraction(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	parent := filepath.Join(root, "Nibfin", "candidates", "core")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	source := sourceFixture(t, parent)
	source.record.Phase = "released"
	if err := source.save(); err != nil {
		t.Fatal(err)
	}
	fixture := upgradeFixture(t, false, true)
	fixtureUpgradePlan(t, fixture)
	app := App{API: fixture, Process: upgradeNoProcess{t}}
	var out bytes.Buffer
	args := []string{"upgrade", "--repo", "example/project"}
	if err := app.Run(args, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source.Directory); err != nil {
		t.Fatal("preview retired a source", err)
	}
	out.Reset()
	if err := app.Run(append(args, "--apply"), &out); err != nil {
		t.Fatal(err)
	}
	sourceGone(t, source)
	if !strings.Contains(out.String(), "source_cleanup") {
		t.Fatal("applied upgrade did not report retirement")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatal("upgrade must not extract a new cache for cleanup", entries, err)
	}
	writes := len(fixture.writes)
	if err := app.Run(append(args, "--apply"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(fixture.writes) != writes {
		t.Fatal("lost cleanup response must not replay confirmed provider writes")
	}
}

func TestSourceLegacyAndSymlinkNamespacePreserved(t *testing.T) {
	parent := t.TempDir()
	legacy := filepath.Join(parent, Pins().Core.Commit)
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacy, filepath.Join(parent, ".source-foreign")); err != nil {
		t.Fatal(err)
	}
	results := sweepCoreSources(parent, observeSourceUse)
	if len(results) != 2 {
		t.Fatal(results)
	}
	for _, result := range results {
		if result.Status != "retained" || result.Reason == "" {
			t.Fatal(result)
		}
	}
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	if err := os.Symlink(parent, filepath.Join(root, "Nibfin")); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceCacheParent(); err == nil {
		t.Fatal("aliased namespace must not authorize cleanup")
	}
}
