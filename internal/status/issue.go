package status

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// issueStateJSON is the part of a single-Issue read the person view's
// search terms need beyond issueJSON.
type issueStateJSON struct {
	issueJSON
	State     string `json:"state"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// ReadIssue re-reads one Issue of the person view on host: repo is
// OWNER/REPO, user the gh login the person view lists Issues for, and
// runners the host's runner report, the source of the execution repository.
//
// listed is whether the Issue still belongs to the person view: an open
// Issue (not a pull request) with the scope label that user is assigned to,
// the terms of the person view's search. Only a listed Issue is built, with
// its pending comment, blockers and runs exactly as status reads them. A
// missing (404) or deleted (410) Issue is not listed and no error.
func ReadIssue(ctx context.Context, host, user string, runners op.RunnersReport, repo string, number int) (Issue, bool, *op.Error) {
	c, err := newREST(host)
	if err != nil {
		return Issue{}, false, op.AsError(op.Errorf(op.CodeAuthUnavailable, "cannot use the gh login for %s: %v", host, err))
	}
	r := &reader{c: c, host: host, runners: runners}
	var j issueStateJSON
	if err := r.get(ctx, fmt.Sprintf("%s/issues/%d", base(repo), number), &j); err != nil {
		var h *api.HTTPError
		if errors.As(err, &h) && (h.StatusCode == http.StatusNotFound || h.StatusCode == http.StatusGone) {
			return Issue{}, false, nil
		}
		return Issue{}, false, apiErr(op.CodeGitHubAPIFailed, fmt.Sprintf("read %s#%d", repo, number), err)
	}
	if !listedIssue(j, repo, user) {
		return Issue{}, false, nil
	}
	is := r.withPending(ctx, repo, toIssue(j.issueJSON))
	issues := []Issue{is}
	r.links(ctx, []*repoGroup{newGroup(repo, issues)})
	return issues[0], true, nil
}

// listedIssue applies the person view's search terms to one read Issue. A
// transferred Issue, which GitHub answers from its new repository, is not
// repo's.
func listedIssue(j issueStateJSON, repo, user string) bool {
	if j.PullRequest != nil || !strings.EqualFold(j.State, "open") || user == "" {
		return false
	}
	if got, ok := repoOfURL(j.RepositoryURL); !ok || !strings.EqualFold(got, repo) {
		return false
	}
	scope, assigned := false, false
	for _, l := range j.Labels {
		scope = scope || strings.EqualFold(l.Name, "scope")
	}
	for _, a := range j.Assignees {
		assigned = assigned || strings.EqualFold(a.Login, user)
	}
	return scope && assigned
}
