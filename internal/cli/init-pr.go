package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type installPR struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	State  string `json:"state"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func createInstallPR(api API, plan InitPlan, branch installBranch) (string, error) {
	base := "repos/" + plan.Repository.FullName
	var head reference
	if err := api.Call("GET", base+"/git/ref/heads/"+url.PathEscape(branch.Name),
		nil, &head); err != nil {
		return "", errors.New("INSTALL_BRANCH_READBACK_FAILED")
	}
	if head.Object.SHA != branch.SHA {
		return "", errors.New("INSTALL_BRANCH_CHANGED_AFTER_VERIFICATION")
	}
	var pull installPR
	body := map[string]any{"title": "Install pinned Nibfin candidate workflows",
		"head": branch.Name, "base": plan.Repository.DefaultBranch,
		"body": "Candidate initialization only. Review source and current CI before " +
			"merging. Host adoption, credentials, isolation and delivery authority " +
			"are separate; these workflows do not establish readiness or E2E."}
	writeErr := api.Call("POST", base+"/pulls", body, &pull)
	if pull.Number <= 0 {
		var observed []installPR
		owner := strings.Split(plan.Repository.FullName, "/")[0]
		query := "?state=open&head=" + url.QueryEscape(owner+":"+branch.Name)
		if err := api.Call("GET", base+"/pulls"+query, nil, &observed); err != nil ||
			len(observed) != 1 {
			return "", errors.New("PR_RESULT_UNKNOWN_READ_BACK_BEFORE_RETRY")
		}
		pull = observed[0]
	}
	var observed installPR
	path := fmt.Sprintf("%s/pulls/%d", base, pull.Number)
	if err := api.Call("GET", path, nil, &observed); err != nil {
		return "", errors.New("PR_READBACK_UNAVAILABLE")
	}
	wantURL := fmt.Sprintf("https://github.com/%s/pull/%d",
		plan.Repository.FullName, pull.Number)
	if observed.Number != pull.Number || observed.State != "open" ||
		observed.URL != wantURL || observed.Head.SHA != branch.SHA ||
		observed.Head.Ref != branch.Name || observed.Base.Ref != plan.Repository.DefaultBranch {
		return "", errors.New("PR_READBACK_MISMATCH")
	}
	_ = writeErr
	return observed.URL, nil
}
