package cli

import (
	"strings"
	"testing"
)

func TestUpgradeGitObjectGoldenIdentities(t *testing.T) {
	// Golden outputs independently obtained using git hash-object --stdin,
	// without creating an index, object database or other repository metadata.
	empty := treeHash(map[string]upgradeEntry{}, "")
	if empty != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Fatal(empty)
	}
	hello := "ce013625030ba8dba906f756967f9e9ca394464a"
	if blobHash([]byte("hello\n")) != hello {
		t.Fatal("blob encoding")
	}
	entries := map[string]upgradeEntry{
		"a": {Path: "a", Mode: "100644", Type: "blob", SHA: hello},
	}
	tree := treeHash(entries, "")
	if tree != "0976950c1fdbcb52435a433913017bf044b3a58f" {
		t.Fatal(tree)
	}
	plan := upgradePlan{Base: strings.Repeat("a", 40), ID: strings.Repeat("0", 64)}
	sha := upgradeCommitID(plan, tree)
	if sha != "f12985f45dc8cf0d24d49a4dcc2a06941f2d9dbc" {
		t.Fatal(sha)
	}
	nested := map[string]upgradeEntry{
		"a":   {Path: "a", Mode: "040000", Type: "tree", SHA: tree},
		"a/a": {Path: "a/a", Mode: "100644", Type: "blob", SHA: hello},
		"a.c": {Path: "a.c", Mode: "100644", Type: "blob", SHA: hello},
	}
	sha = treeHash(nested, "")
	if sha != "cfbbc9b898c4a7003d387764c07b30ccd8e6fbf5" {
		t.Fatal(sha)
	}
}

func TestUpgradeRejectsAmbiguousAndUnsupportedPinSchemas(t *testing.T) {
	for _, data := range []string{`{"core":{},"core":{}}`,
		`{"core":{"commit":"a","commit":"b"}}`, `{"newSchema":2}`, `{"core":{},"Core":{}}`,
		`{}` + `{}`, `null`, `[]`} {
		var pins Producers
		err := decodeUpgradePins([]byte(data), &pins)
		// Empty/null values cannot become a source even if syntactically decodable.
		if err == nil {
			f := upgradeFixture(t, false, true)
			if _, err := upgradeRecipes(f, pins); err == nil {
				t.Fatal(data)
			}
		}
	}
}
