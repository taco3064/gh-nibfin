package status

import "github.com/taco3064/gh-nibfin/internal/op"

// PersonView and RepoView declare the top-level keys of the two status
// Results, with the Go type of each value. ReadBack builds the Result as an
// op.Result map with exactly these keys; they are the source of the
// published JSON Schema (Schema). omitempty marks the keys ReadBack sets
// only sometimes.

// PersonView is the Result of status without a repository.
type PersonView struct {
	Host             op.HostReport `json:"host"`
	LatestImage      LatestImage   `json:"latestImage"`
	GitHubHost       string        `json:"githubHost"`
	User             string        `json:"user,omitempty"`
	UserError        *op.Error     `json:"userError,omitempty"`
	Repos            []RepoIssues  `json:"repos"`
	Truncated        bool          `json:"truncated"`
	IssuesError      *op.Error     `json:"issuesError,omitempty"`
	NibfinRepos      []NibfinRepo  `json:"nibfinRepos"`
	NibfinReposError *op.Error     `json:"nibfinReposError,omitempty"`
}

// RepoView is the Result of status with a repository.
type RepoView struct {
	Host             op.HostReport `json:"host"`
	LatestImage      LatestImage   `json:"latestImage"`
	Repo             string        `json:"repo"`
	Image            RepoImage     `json:"image"`
	Issues           []Issue       `json:"issues"`
	IssuesError      *op.Error     `json:"issuesError,omitempty"`
	NibfinRun        NibfinRun     `json:"nibfinRun"`
	Permission       *string       `json:"permission"`
	PermissionError  *op.Error     `json:"permissionError,omitempty"`
	NibfinRepos      []NibfinRepo  `json:"nibfinRepos"`
	NibfinReposError *op.Error     `json:"nibfinReposError,omitempty"`
}
