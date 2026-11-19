package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func replaceSourceRecord(t *testing.T, source *coreSource) []byte {
	t.Helper()
	data := []byte(`{"Kind":"foreign","important":"preserve this record"}`)
	path := filepath.Join(t.TempDir(), "replacement")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(source.parent, source.name,
		sourceRecordName)); err != nil {
		t.Fatal(err)
	}
	return data
}

func assertSourceRecordPreserved(t *testing.T, source *coreSource, expected []byte) {
	t.Helper()
	data, err := source.root.ReadFile(sourceRecordName)
	if err != nil || !bytes.Equal(data, expected) {
		t.Fatal("foreign ownership record changed", string(data), err)
	}
}

func TestSourceCapturePreservesRecordReplacementAfterClassification(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	if _, err := classifiedSourcePaths(source.root, source.record); err != nil {
		t.Fatal(err)
	}
	expected := replaceSourceRecord(t, source)
	if err := captureSourceEntry(source.root, "source/.node-version",
		source.record); err == nil {
		t.Fatal("capture adopted a foreign ownership record")
	}
	assertSourceRecordPreserved(t, source, expected)
	if _, err := os.Stat(filepath.Join(source.Directory, ".node-version")); err != nil {
		t.Fatal("capture proceeded despite changed ownership", err)
	}
}

func TestSourceSavePreservesReplacementAfterOpeningOwnedRecord(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	file, err := source.root.OpenFile(sourceRecordName, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// Replace after save has selected its write target. A replacing rename here
	// used to unlink foreign data; descriptor writes only touch the owned inode.
	expected := replaceSourceRecord(t, source)
	if _, err := writeOwnedSourceRecord(file, source.recordInfo,
		[]byte(`{"updated":"owned record"}`)); err != nil {
		t.Fatal(err)
	}
	assertSourceRecordPreserved(t, source, expected)
	if err := source.save(); err == nil {
		t.Fatal("save accepted a replacement before opening")
	}
	assertSourceRecordPreserved(t, source, expected)
}

func TestSourceSaveRejectsOpenedReplacement(t *testing.T) {
	source := sourceFixture(t, t.TempDir())
	expected := replaceSourceRecord(t, source)
	file, err := source.root.OpenFile(sourceRecordName, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := writeOwnedSourceRecord(file, source.recordInfo,
		[]byte("must not be written")); err == nil {
		t.Fatal("save wrote through a substituted descriptor")
	}
	assertSourceRecordPreserved(t, source, expected)
}
