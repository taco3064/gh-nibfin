package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func applyDriver(d *rebuildDriver, out *bytes.Buffer) error {
	return (App{API: d}).applyRebuild(
		d.api.data["repos/example/project"].(Repository),
		d.api.data["user"].(User), "claude", []byte("fixture-secret"),
		json.NewEncoder(out), d.operations())
}

func TestRebuildRetiresBeforePreparationAndRequiresFinalActivation(t *testing.T) {
	d := newRebuildDriver(t)
	d.lostRemoval = true
	var out bytes.Buffer
	if err := applyDriver(d, &out); err != nil {
		t.Fatal(err, out.String(), d.calls)
	}
	if !slices.Equal(d.deletes, []int64{25, 26}) ||
		strings.Join(d.calls[:4], ",") !=
			"retire-shared-host,retire-shared-host,retire-shared-host,prepare" ||
		d.calls[len(d.calls)-1] != "activate-shared-replacement" {
		t.Fatal(d.calls, d.deletes)
	}
	for _, expected := range []string{"MANAGED_REPLACEMENT_ACTIVE", "core_verified",
		"fixture-model", "fixture-profile", `"id":31`, `"id":32`, "example/peer"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatal(out.String())
		}
	}
	path, _ := sharedMutationPath()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed intent cleanup failed", err)
	}
	for _, path := range d.paths {
		for _, name := range stagedSecrets {
			if _, err := os.Lstat(filepath.Join(path, name)); !os.IsNotExist(err) {
				t.Fatal("staged credential cleanup failed", err)
			}
		}
	}
	if strings.Contains(out.String(), "fixture-secret") {
		t.Fatal("private provider input in public output")
	}
}

func TestRebuildInterruptionsResumeExactPoolAndRegistrations(t *testing.T) {
	for _, failure := range []string{"register", "adopt", "peer", "start", "wake",
		"activate", "lost-activation", "publishing"} {
		t.Run(failure, func(t *testing.T) {
			d := newRebuildDriver(t)
			d.failure = failure
			d.lostAdoption = failure == "adopt" || failure == "publishing"
			d.lostActivation = failure == "lost-activation"
			var out bytes.Buffer
			if err := applyDriver(d, &out); err == nil {
				t.Fatal("interruption reported completion", out.String())
			}
			if strings.Contains(out.String(), "core_verified") {
				t.Fatal("preparation was confused with final activation")
			}
			path, _ := sharedMutationPath()
			saved, err := os.ReadFile(path)
			if err != nil || bytes.Contains(saved, []byte("fixture-secret")) {
				t.Fatal("intent missing or contains provider material", err)
			}
			if err := requireNoReplacement(); err == nil {
				t.Fatal("ordinary initialization accepted pending replacement")
			}
			d.failure = ""
			out.Reset()
			if err := applyDriver(d, &out); err != nil {
				t.Fatal(err, d.calls, out.String())
			}
			if len(d.runners) != 2 || d.runners[0].ID != 31 || d.runners[1].ID != 32 ||
				!slices.Equal(d.deletes, []int64{25, 26}) {
				t.Fatal("resume duplicated or changed native effects", d.runners, d.deletes)
			}
		})
	}
}

func TestRebuildPreviewAndUnmetPrerequisitesHaveNoEffects(t *testing.T) {
	for _, mode := range []string{"preview", "denied", "incompatible", "composition"} {
		t.Run(mode, func(t *testing.T) {
			d := newRebuildDriver(t)
			d.denied, d.incompatible = mode == "denied", mode == "incompatible"
			if mode == "composition" {
				d.binding.Model = ""
			}
			repo := d.api.data["repos/example/peer"].(Repository)
			user := d.api.data["user"].(User)
			_, unmet, err := previewRebuild(repo, user, "claude", d.operations())
			if err != nil || mode == "preview" && len(unmet) != 0 ||
				mode != "preview" && len(unmet) == 0 {
				t.Fatal(err, unmet)
			}
			path, _ := sharedMutationPath()
			if _, err := os.Lstat(path); !os.IsNotExist(err) || len(d.calls) != 0 ||
				len(d.deletes) != 0 || len(d.api.writes) != 0 {
				t.Fatal("preview advanced host or intent")
			}
			if mode != "preview" {
				var out bytes.Buffer
				if err := applyDriver(d, &out); err == nil || len(d.calls) != 0 {
					t.Fatal("unmet prerequisite permitted effects", err)
				}
			}
		})
	}
}

func TestRebuildUnresolvedSettlementDoesNotRemoveOrPrepare(t *testing.T) {
	d := newRebuildDriver(t)
	d.pending = true
	var out bytes.Buffer
	if err := applyDriver(d, &out); err == nil || len(d.deletes) != 0 ||
		slices.Contains(d.calls, "prepare") {
		t.Fatal(err, d.calls, d.deletes)
	}
	d.pending = false
	// Fixture observes settlement before granting the next fresh removal response.
	d.retirement.Phase = "removal"
	if err := applyDriver(d, &out); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildIntentConflictsAndPeerSerialization(t *testing.T) {
	d := newRebuildDriver(t)
	d.pending = true
	var out bytes.Buffer
	_ = applyDriver(d, &out)
	ops := d.operations()
	repo := d.api.data["repos/example/peer"].(Repository)
	user := d.api.data["user"].(User)
	if _, _, err := previewRebuild(repo, user, "codex", ops); err == nil {
		t.Fatal("conflicting explicit binding accepted")
	}
	unlock, err := sharedMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := applyDriver(d, &out); err == nil {
		t.Fatal("parallel apply accepted")
	}
	plan := InitPlan{Execution: &d.old.SharedExecution}
	if close, err := lockSharedInit(plan); err == nil {
		close()
		t.Fatal("peer installation not serialized")
	}
}
