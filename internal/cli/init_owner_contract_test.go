package cli

import (
	"strings"
	"testing"
)

func TestBothOwnersDenyRevocationWorkflowWritesAndExistingConflicts(t *testing.T) {
	for _, ownerType := range []string{"User", "Organization"} {
		for _, kind := range []string{"private", "revoked", "workflow-write",
			"existing-conflict", "stale-head", "changed-producer-pin"} {
			f := installFixture(t, ownerType)
			switch kind {
			case "private":
				repo := f.data["repos/example/project"].(Repository)
				repo.Private = true
				f.data["repos/example/project"] = repo
			case "revoked":
				repo := f.data["repos/example/project"].(Repository)
				repo.Permissions = nil
				f.data["repos/example/project"] = repo
			case "workflow-write":
				f.deniedPath = "repos/example/project/git/refs"
			case "existing-conflict":
				path := "repos/example/project/contents/" + f.plan.Files[0].Path
				f.data[path+"?ref="+f.plan.Head] = encoded([]byte("unapproved change"))
			case "stale-head":
				ref := reference{}
				ref.Object.SHA = strings.Repeat("e", 40)
				f.data["repos/example/project/git/ref/heads/main"] = ref
			case "changed-producer-pin":
				pin := Pins().Action
				path := "repos/" + pin.Repository + "/contents/core-source.json"
				f.data[path+"?ref="+pin.Commit] = encoded([]byte("{}"))
			}
			result, err := applyInit(f, f.plan)
			if err == nil || result.Status != "pending" || result.PR != "" {
				t.Fatal("denied installation must never claim successful apply")
			}
			for _, write := range f.writes {
				if strings.HasSuffix(write, "/pulls") ||
					kind != "workflow-write" {
					t.Fatal("denied or conflicting installation must not publish a PR")
				}
			}
		}
	}
}
