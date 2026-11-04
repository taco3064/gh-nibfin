package cli

import (
	"errors"
	"net/url"
)

type installBranch struct {
	Name string
	SHA  string
}

func createInstallBranch(api API, base string, plan InitPlan) (installBranch, error) {
	branch := "nibfin/init-" + plan.Head[:12]
	var existing reference
	err := api.Call("GET", base+"/git/ref/heads/"+url.PathEscape(branch), nil, &existing)
	if err == nil {
		return installBranch{}, errors.New("INSTALL_BRANCH_EXISTS_INSPECT_BEFORE_RETRY")
	}
	if err.Error() != "NOT_FOUND" {
		return installBranch{}, err
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := api.Call("GET", base+"/git/commits/"+plan.Head, nil, &commit); err != nil {
		return installBranch{}, err
	}
	if len(commit.Tree.SHA) != 40 {
		return installBranch{}, errors.New("TARGET_TREE_UNAVAILABLE")
	}
	entries := make([]map[string]string, 0, len(plan.Files))
	for _, file := range plan.Files {
		entries = append(entries, map[string]string{"path": file.Path, "mode": "100644",
			"type": "blob", "content": file.Content})
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if err := api.Call("POST", base+"/git/trees", map[string]any{
		"base_tree": commit.Tree.SHA, "tree": entries}, &tree); err != nil {
		return installBranch{}, errors.New("INSTALL_TREE_RESULT_UNKNOWN")
	}
	if len(tree.SHA) != 40 {
		return installBranch{}, errors.New("INSTALL_TREE_UNAVAILABLE")
	}
	var created struct {
		SHA string `json:"sha"`
	}
	if err := api.Call("POST", base+"/git/commits", map[string]any{
		"message": "Install verified Nibfin candidate workflows",
		"tree":    tree.SHA, "parents": []string{plan.Head}}, &created); err != nil {
		return installBranch{}, errors.New("INSTALL_COMMIT_RESULT_UNKNOWN")
	}
	if len(created.SHA) != 40 {
		return installBranch{}, errors.New("INSTALL_COMMIT_UNAVAILABLE")
	}
	writeErr := api.Call("POST", base+"/git/refs", map[string]string{
		"ref": "refs/heads/" + branch, "sha": created.SHA}, nil)
	var observed reference
	if err := api.Call("GET", base+"/git/ref/heads/"+url.PathEscape(branch),
		nil, &observed); err != nil || observed.Object.SHA != created.SHA {
		return installBranch{}, errors.New(
			"INSTALL_BRANCH_RESULT_UNKNOWN_READ_BACK_BEFORE_RETRY")
	}
	_ = writeErr
	for _, file := range plan.Files {
		data, _, err := content(api, plan.Repository.FullName, file.Path, created.SHA)
		if err != nil || string(data) != file.Content {
			return installBranch{}, errors.New("INSTALLATION_CONTENT_READBACK_MISMATCH")
		}
	}
	return installBranch{Name: branch, SHA: created.SHA}, nil
}
