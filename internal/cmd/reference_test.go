package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// ReferenceFile is the committed command reference the website's CLI page
// renders. It is commandSchema's output, the same data /api/commands serves.
const ReferenceFile = "dashboard/src/app/data/commands.json"

var updateReference = flag.Bool("update", false, "write "+ReferenceFile+" from the cobra commands")

// referencePath is ReferenceFile from this package's directory.
var referencePath = filepath.Join("..", "..", filepath.FromSlash(ReferenceFile))

const regenerateReference = "go test ./internal/cmd -run TestCommandReferenceFile -update"

// commandReference is the bytes ReferenceFile holds.
func commandReference() ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false) // keep <user id> readable in the file and its diffs
	e.SetIndent("", "  ")
	if err := e.Encode(commandSchema(testRoot())); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// TestCommandReferenceFile fails when the committed reference differs from
// the cobra command tree; with -update it writes the file.
func TestCommandReferenceFile(t *testing.T) {
	want, err := commandReference()
	if err != nil {
		t.Fatal(err)
	}
	if *updateReference {
		if err := os.MkdirAll(filepath.Dir(referencePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(referencePath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("read %s: %v; run %s", ReferenceFile, err, regenerateReference)
	}
	// A Windows checkout can turn the file's line endings into CRLF.
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale against the cobra commands; run %s", ReferenceFile, regenerateReference)
	}
}
