package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestManagedInitRecoversAnAlreadyMergedInstallationPR(t *testing.T) {
	const repo = "example/project"
	const workflow = ".github/workflows/nibfin-issue.yml"
	const apiPath = "repos/" + repo
	head, merge := strings.Repeat("b", 40), strings.Repeat("c", 40)
	prURL := "https://github.com/" + repo + "/pull/7"
	original := InitPlan{
		Repository: Repository{ID: 10, FullName: repo, DefaultBranch: "main",
			Owner: User{Login: "example"}},
		Head:  head,
		Files: []Change{{Path: workflow, Content: "protected workflow"}},
	}
	branch := "nibfin/init-" + original.Head[:12]
	query := apiPath + "/pulls?state=all&per_page=2&head=example%3A" +
		strings.ReplaceAll(branch, "/", "%2F")
	pr := map[string]any{
		"number": 7, "html_url": prURL, "state": "closed", "merged": true,
		"head": map[string]any{"ref": branch, "sha": head,
			"repo": map[string]any{"id": 10}},
		"base": map[string]any{"ref": "main", "repo": map[string]any{"id": 10}},
	}
	mainBranch := map[string]any{"protected": true,
		"commit": map[string]string{"sha": merge}}
	contentPath := apiPath + "/contents/" + workflow + "?ref=" + merge
	headContentPath := apiPath + "/contents/" + workflow + "?ref=" + head
	api := &fakeAPI{data: map[string]any{
		query:                      []map[string]int{{"number": 7}},
		apiPath + "/pulls/7":       pr,
		apiPath + "/branches/main": mainBranch,
		contentPath:                encoded([]byte(original.Files[0].Content)),
		headContentPath:            encoded([]byte(original.Files[0].Content)),
	}}
	intent := managedInstallIntent{Plan: original}
	var saved bool
	var published string
	save := func() error { saved = true; return nil }
	publish := func(url string) error { published = url; return nil }
	ctx := context.Background()
	if err := waitManagedInstall(ctx, api, &intent, save, publish); err != nil {
		t.Fatal(err)
	}
	if !saved || intent.PR != 7 || intent.Head != head || published != prURL ||
		len(api.writes) != 0 {
		t.Fatalf("expected the existing PR without writes, got %+v and %v", intent, api.writes)
	}
	mainBranch["protected"] = false
	err := waitManagedInstall(ctx, api, &intent, save, publish)
	if err == nil || err.Error() != "MANAGED_INSTALL_DEFAULT_BRANCH_UNPROTECTED" {
		t.Fatalf("expected branch protection to be required, got %v", err)
	}

	mainBranch["protected"] = true
	pr["state"], pr["merged"] = "open", false
	intent = managedInstallIntent{Plan: original}
	var checkpoint managedInstallIntent
	interrupted, cancel := context.WithCancel(ctx)
	cancel()
	err = waitManagedInstall(interrupted, api, &intent, func() error {
		checkpoint = intent
		return nil
	}, publish)
	var pending *CommandError
	if !errors.As(err, &pending) || pending.Code != "MANAGED_INSTALL_AWAITING_PR_MERGE" ||
		checkpoint.PR != 7 || checkpoint.Head != head {
		t.Fatalf("interrupted wait lost the PR identity, got %+v, %v", checkpoint, err)
	}
	delete(api.data, query)
	pr["state"], pr["merged"] = "closed", true
	if err := waitManagedInstall(ctx, api, &checkpoint, func() error {
		t.Fatal("resuming the stored intent must not replace it")
		return nil
	}, publish); err != nil || len(api.writes) != 0 {
		t.Fatalf("resuming the same PR failed or wrote to GitHub: %v, %v", err, api.writes)
	}
}
