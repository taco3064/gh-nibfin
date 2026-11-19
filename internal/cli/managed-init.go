package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Only nonsecret observations enter the installation intent. Metadata is an
// observation of our confirmed write, never a credential equality/validity proof.
type nativeCredentialObservation struct {
	State     string `json:"state,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type managedInstallIntent struct {
	Native nativeCredentialObservation `json:"native,omitempty"`
	Plan   InitPlan                    `json:"plan"`
	Pins   Producers                   `json:"pins"`
	PR     int                         `json:"pull_request,omitempty"`
	Head   string                      `json:"pull_head,omitempty"`
}

type observedInstallPR struct {
	installPR
	Merged bool `json:"merged"`
}

func observeInstallIntent(api API, x *managedInstallIntent) (observedInstallPR, error) {
	var observed observedInstallPR
	base := "repos/" + x.Plan.Repository.FullName + "/pulls"
	if x.PR > 0 {
		err := api.Call("GET", fmt.Sprintf("%s/%d", base, x.PR), nil, &observed)
		return observed, err
	}
	// The durable plan precedes PR creation. This lookup also recovers a
	// merged PR if the host stopped before it could save the PR number.
	branch := "nibfin/init-" + x.Plan.Head[:12]
	head := x.Plan.Repository.Owner.Login + ":" + branch
	var prs []installPR
	err := api.Call("GET", base+"?state=all&per_page=2&head="+url.QueryEscape(head),
		nil, &prs)
	if err != nil || len(prs) > 1 {
		return observed, errors.New("MANAGED_INSTALL_PR_REQUIRES_READBACK")
	}
	if len(prs) == 0 {
		return observed, nil
	}
	err = api.Call("GET", fmt.Sprintf("%s/%d", base, prs[0].Number), nil, &observed)
	return observed, err
}

func verifyInstallPR(api API, x *managedInstallIntent, pr observedInstallPR) error {
	plan := x.Plan
	branch := "nibfin/init-" + plan.Head[:12]
	wantURL := fmt.Sprintf("https://github.com/%s/pull/%d",
		plan.Repository.FullName, pr.Number)
	if pr.Number <= 0 || pr.URL != wantURL || pr.Head.Ref != branch ||
		!commitPattern.MatchString(pr.Head.SHA) ||
		pr.Head.Repo.ID != plan.Repository.ID || pr.Base.Repo.ID != plan.Repository.ID ||
		pr.Base.Ref != plan.Repository.DefaultBranch ||
		x.PR > 0 && (pr.Number != x.PR || pr.Head.SHA != x.Head) {
		return errors.New("MANAGED_INSTALL_PR_CHANGED")
	}
	ref := pr.Head.SHA
	if pr.Merged && pr.State == "closed" {
		var branch struct {
			Protected bool `json:"protected"`
			Commit    struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		path := "repos/" + plan.Repository.FullName + "/branches/" +
			url.PathEscape(plan.Repository.DefaultBranch)
		if err := api.Call("GET", path, nil, &branch); err != nil ||
			!commitPattern.MatchString(branch.Commit.SHA) {
			return errors.New("MANAGED_INSTALL_DEFAULT_BRANCH_UNAVAILABLE")
		}
		if !branch.Protected && !plan.NativeManaged {
			return errors.New("MANAGED_INSTALL_DEFAULT_BRANCH_UNPROTECTED")
		}
		ref = branch.Commit.SHA
	} else if pr.Merged || pr.State != "open" {
		return errors.New("MANAGED_INSTALL_PR_CLOSED_WITHOUT_MERGE")
	}
	for _, file := range plan.Files {
		data, _, err := content(api, plan.Repository.FullName, file.Path, ref)
		if err != nil || string(data) != file.Content {
			return errors.New("MANAGED_INSTALL_WORKFLOW_READBACK_MISMATCH")
		}
	}
	return nil
}

func waitManagedInstall(ctx context.Context, api API, x *managedInstallIntent,
	save func() error, publish func(string) error) error {
	pr, err := observeInstallIntent(api, x)
	if err != nil {
		return err
	}
	if pr.Number == 0 {
		if _, err := applyInit(api, x.Plan); err != nil {
			return err
		}
		pr, err = observeInstallIntent(api, x)
		if err != nil {
			return err
		}
	}
	if err := verifyInstallPR(api, x, pr); err != nil {
		return err
	}
	if x.PR == 0 {
		x.PR, x.Head = pr.Number, pr.Head.SHA
		if err := save(); err != nil {
			return err
		}
	}
	if err := publish(pr.URL); err != nil {
		return err
	}
	for !pr.Merged {
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &CommandError{Code: "MANAGED_INSTALL_AWAITING_PR_MERGE", Exit: 2}
		case <-timer.C:
		}
		pr, err = observeInstallIntent(api, x)
		if err != nil {
			return err
		}
		if err := verifyInstallPR(api, x, pr); err != nil {
			return err
		}
	}
	return nil
}

func hostRepository(host sharedHost) Repository {
	return Repository{ID: host.RepositoryID, FullName: host.Repository}
}
