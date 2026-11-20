package cli

import (
	"bytes"
	"io"
	"net/url"
	"runtime"
	"strings"
	"testing"
)

func TestUpgradeLegacyRebasePublicContinuation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("public Linux upgrade; Windows delegates through WSL")
	}
	checkUpgradeLegacyContinuation(t, func(a App, args []string, out io.Writer) error {
		return a.Run(args, out)
	})
}

func TestUpgradeLegacyRebaseLinuxContinuation(t *testing.T) {
	checkUpgradeLegacyContinuation(t, func(a App, args []string, out io.Writer) error {
		return a.routeUpgrade(args[1:], "linux", out)
	})
}

func checkUpgradeLegacyContinuation(t *testing.T,
	run func(App, []string, io.Writer) error) {
	t.Helper()
	modes := []string{"response", "lost-response", "lost-observation",
		"merge-response", "merge-lost-response", "merge-lost-observation",
		"member-response", "member-lost-response", "member-lost-observation"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			f := upgradeFixture(t, false, true)
			plan := fixtureUpgradePlan(t, f)
			if _, err := applyUpgrade(f, plan, "latest"); err != nil {
				t.Fatal(err)
			}
			pull := f.data[upgradeRepo+"/pulls/9"].(upgradePull)
			key := upgradeRepo + "/git/commits/" + pull.Head.SHA
			commit := f.data[key].(upgradeCommit)
			commit.Message = strings.Split(commit.Message, "Sources: ")[0]
			f.data[key] = commit
			current := advanceUpgradeBase(t, f, plan)
			if strings.HasPrefix(mode, "merge-") || strings.HasPrefix(mode, "member-") {
				addUpgradeSideParent(t, f, current, strings.HasPrefix(mode, "member-"))
			}
			if strings.Contains(mode, "lost-") {
				f.lost = "graphql"
			}
			if strings.HasSuffix(mode, "lost-observation") {
				f.denyAfterWrite = upgradeRepo + "/git/ref/heads/" +
					url.PathEscape(upgradeBranchName)
			}
			app := App{API: f, Process: upgradeNoProcess{t}}
			args := []string{"upgrade", "--repo", "example/project", "--apply"}
			writes := len(f.writes)
			var out bytes.Buffer
			err := run(app, args, &out)
			if (err != nil) != (strings.HasSuffix(mode, "lost-observation")) {
				t.Fatal(out.String(), err)
			}
			if len(f.writes) != writes+1 || f.writes[writes] != "POST graphql" {
				t.Fatal("expected only native rebase", f.writes[writes:])
			}
			f.denyRead = ""
			out.Reset()
			if err := run(app, args, &out); err != nil {
				t.Fatal("confirmed legacy rebase must remain recoverable", out.String(), err)
			}
			if !strings.Contains(out.String(), pull.URL) || len(f.writes) != writes+1 {
				t.Fatal("same PR must be reused without writes", out.String(), f.writes)
			}
			// An unavailable original binding is pending, never presumed verified.
			f.denyRead = upgradeRepo + "/git/commits/" + plan.Base
			out.Reset()
			if err := run(app, args, &out); err == nil ||
				!strings.Contains(out.String(), "READ_BACK_UNAVAILABLE") {
				t.Fatal("missing ancestry must remain pending", out.String(), err)
			}
			f.denyRead = ""
			if err := run(app, args, &bytes.Buffer{}); err != nil ||
				len(f.writes) != writes+1 {
				t.Fatal("restored observation must reuse confirmed effect", err, f.writes)
			}
		})
	}
}

// Side ancestry can predate installation or carry another member's installation.
// Neither side supplies the original plan, which remains on the main ancestry.
func addUpgradeSideParent(t *testing.T, f *upgradeAPI, current upgradePlan,
	installed bool) {
	t.Helper()
	side := upgradeCommit{SHA: strings.Repeat("c", 40)}
	entries := map[string]upgradeEntry{}
	if installed {
		recipes, err := upgradeRecipes(f, f.old)
		if err != nil {
			t.Fatal(err)
		}
		for path, entry := range f.baseEntries {
			entries[path] = entry
		}
		for path, text := range templatesForRecipe(f.old, 43, recipes, "issue") {
			entries[path] = upgradeEntry{Path: path, Mode: "100644", Type: "blob",
				SHA: blobHash([]byte(text))}
			f.data[upgradeRepo+"/contents/"+path+"?ref="+side.SHA] = encoded([]byte(text))
		}
	}
	side.Tree.SHA = fixtureTree(f.fakeAPI, entries)
	f.data[upgradeRepo+"/git/commits/"+side.SHA] = side
	if installed {
		other, err := planUpgradeAt(f, "example/project", "latest", false, side.SHA)
		if err != nil || other.MemberID != 43 || sameUpgradeIntent(current, other) {
			t.Fatal("side must have a verified but different member intent", other, err)
		}
	}
	key := upgradeRepo + "/git/commits/" + current.Base
	merge := f.data[key].(upgradeCommit)
	merge.Parents = append(merge.Parents, struct {
		SHA string `json:"sha"`
	}{side.SHA})
	f.data[key] = merge
}
