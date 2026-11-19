package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T, parent string) *coreSource {
	t.Helper()
	source, err := reserveCoreSource(parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { source.root.Close() })
	archive := testArchive(t, []string{"bin/nibfin-core.mjs", ".node-version"})
	if err := source.extract(archive); err != nil {
		t.Fatal(err)
	}
	return source
}

func sourceGone(t *testing.T, source *coreSource) {
	t.Helper()
	if _, err := os.Lstat(filepath.Dir(source.Directory)); !os.IsNotExist(err) {
		t.Fatal("disposable operation source remains", err)
	}
}

func TestSourceReleaseAndSimultaneousOwners(t *testing.T) {
	parent := t.TempDir()
	first, otherRepository := sourceFixture(t, parent), sourceFixture(t, parent)
	if first.Directory == otherRepository.Directory {
		t.Fatal("simultaneous operations must not share an extraction")
	}
	results := sweepCoreSources(parent, observeSourceUse)
	if len(results) != 2 {
		t.Fatal(results)
	}
	for _, result := range results {
		if result.Status != "deferred" || !strings.Contains(result.Reason, "running") {
			t.Fatal("another operation/repository must retain its selected source", result)
		}
	}
	if result := first.Release(); result.Status != "removed" {
		t.Fatal(result)
	}
	sourceGone(t, first)
	if result := first.Release(); result.Status != "removed" {
		t.Fatal("repeated release must reuse confirmed removal", result)
	}
	if _, err := os.Stat(otherRepository.Directory); err != nil {
		t.Fatal("release removed another operation's source", err)
	}
	if result := otherRepository.Release(); result.Status != "removed" {
		t.Fatal(result)
	}
	if results := sweepCoreSources(parent, observeSourceUse); len(results) != 0 {
		t.Fatal("confirmed removals must be reused after response loss", results)
	}
}

func TestSourceInterruptedAndPartialRetirement(t *testing.T) {
	parent := t.TempDir()
	source := sourceFixture(t, parent)
	// Simulate a terminated owner: the current PID's different start identity
	// must not be mistaken for the source's original operation.
	source.record.Owner.Start = "0"
	source.record.Phase = "verified"
	if err := source.save(); err != nil {
		t.Fatal(err)
	}
	if err := source.root.Remove("source/.node-version"); err != nil {
		t.Fatal(err)
	}
	source.root.Close()
	results := sweepCoreSources(parent, observeSourceUse)
	if len(results) != 1 || results[0].Status != "removed" {
		t.Fatal("partial retirement should resume from remaining filesystem state", results)
	}
	sourceGone(t, source)
	result := retireCoreSource(parent, source.name, source.root, source.record,
		func(sourceRecord) string { return "" })
	if result.Status != "removed" || result.Reason != "already absent" {
		t.Fatal(result)
	}
}

func TestSourceUnknownAndRetainedState(t *testing.T) {
	for _, scenario := range []string{"handoff", "starting", "extracting", "boot",
		"identity", "consumer", "phase"} {
		t.Run(scenario, func(t *testing.T) {
			source := sourceFixture(t, t.TempDir())
			source.record.Owner.Start = "0"
			source.record.Phase = "verified"
			switch scenario {
			case "handoff":
				source.record.Retained = "supported handoff still consumes source"
			case "starting", "extracting":
				source.record.Phase = scenario
			case "boot":
				source.record.Owner.Boot = "another namespace"
			case "identity":
				source.record.Owner = sourceOwner{}
			case "consumer":
				source.record.Group = -1
			case "phase":
				source.record.Phase = "unknown"
			}
			result := retireCoreSource(source.parent, source.name, source.root,
				source.record, observeSourceUse)
			if result.Status == "removed" || result.Reason == "" {
				t.Fatal(result)
			}
			if _, err := os.Stat(source.Directory); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSourceForeignEntriesAndSubstitutionPreserved(t *testing.T) {
	for _, scenario := range []string{"addition", "edit", "symlink", "hardlink",
		"record", "missing-record", "substitution"} {
		t.Run(scenario, func(t *testing.T) {
			parent := t.TempDir()
			source := sourceFixture(t, parent)
			foreign := filepath.Join(parent, "foreign")
			if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(source.Directory, ".node-version")
			switch scenario {
			case "addition":
				if err := os.WriteFile(filepath.Join(source.Directory, "user-data"),
					[]byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "edit":
				if err := os.WriteFile(path, []byte("foreign edit"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink", "hardlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				link := os.Symlink
				if scenario == "hardlink" {
					link = os.Link
				}
				if err := link(foreign, path); err != nil {
					t.Fatal(err)
				}
			case "record":
				if err := os.WriteFile(filepath.Join(parent, source.name,
					sourceRecordName), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-record":
				if err := source.root.Remove(sourceRecordName); err != nil {
					t.Fatal(err)
				}
			case "substitution":
				if err := os.Rename(filepath.Dir(source.Directory),
					filepath.Join(parent, "moved")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(parent, source.name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			source.record.Phase = "released"
			if scenario != "record" && scenario != "missing-record" &&
				scenario != "substitution" {
				if err := source.save(); err != nil {
					t.Fatal(err)
				}
			}
			result := retireCoreSource(parent, source.name, source.root, source.record,
				func(sourceRecord) string { return "" })
			// Direct retirement uses a record already read by the caller. Sweep
			// owns missing/malformed-record recognition instead.
			if scenario == "record" || scenario == "missing-record" {
				results := sweepCoreSources(parent, observeSourceUse)
				if len(results) == 0 {
					t.Fatal("unclassified source was removed", result)
				}
			} else if result.Status == "removed" {
				t.Fatal("foreign or substituted data removed", result)
			}
			data, err := os.ReadFile(foreign)
			if err != nil || string(data) != "keep" {
				t.Fatal("foreign target changed", err)
			}
		})
	}
}

type sourceVerifierFixture struct{ failure bool }

func (p sourceVerifierFixture) Run(string, []string, []byte) ([]byte, error) {
	if p.failure {
		return nil, errors.New("controlled verification failure")
	}
	return json.Marshal(map[string]any{"status": "verified", "source": Pins().Core})
}

func (p sourceVerifierFixture) runSourceVerifier(name string, args []string,
	started func(int) error) ([]byte, error) {
	if err := started(0); err != nil {
		return nil, err
	}
	return p.Run(name, args, nil)
}

func TestSourceControlledVerificationAndFailureRelease(t *testing.T) {
	for _, failure := range []bool{false, true} {
		source := sourceFixture(t, t.TempDir())
		process := sourceVerifier{sourceVerifierFixture{failure}, source}
		_, err := verifyCache(process, source.Directory, "trusted-bootstrap",
			"pin", Pins().Core)
		if (err != nil) != failure {
			t.Fatal(err)
		}
		if _, err := os.Stat(source.Directory); err != nil {
			t.Fatal("verification must not dispose the owning operation's source", err)
		}
		if result := source.Release(); result.Status != "removed" {
			t.Fatal(result)
		}
		sourceGone(t, source)
	}
	var out bytes.Buffer
	reportSourceCleanup(&out, []sourceCleanupResult{{"entry", "deferred", "unknown"}})
	if !strings.Contains(out.String(), "unknown") {
		t.Fatal("cleanup reason not reported")
	}
}

func TestSourceParentSubstitutionAfterClassification(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	if _, err := classifiedSourcePaths(source.root, source.record); err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	path := filepath.Join(foreign, "nibfin-core.mjs")
	if err := os.WriteFile(path, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := source.root.Rename("source/bin", "source/moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(source.Directory, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := removeClassifiedSourceEntry(source.root, "source/bin/nibfin-core.mjs",
		source.record); err == nil {
		t.Fatal("substituted parent must not redirect deletion")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign" {
		t.Fatal("substituted parent redirected removal", err)
	}
}

func TestSourceReleasePreservesSubstitutedOwnershipRecord(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	path := filepath.Join(t.TempDir(), "foreign-record")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := source.root.Remove(sourceRecordName); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(source.Directory), sourceRecordName)
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if result := source.Release(); result.Status != "deferred" {
		t.Fatal("release must not overwrite a foreign record", result)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("foreign symlink was replaced", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep" {
		t.Fatal("foreign record changed", err)
	}
}
