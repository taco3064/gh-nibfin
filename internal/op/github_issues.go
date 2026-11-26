package op

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
)

// ghUser is the authenticated user.
type ghUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// repoPerms are the current gh login's permissions on a repository.
type repoPerms struct {
	Admin bool `json:"admin"`
	Push  bool `json:"push"`
	// Private is the repository's own private flag, read beside the
	// permissions.
	Private bool `json:"-"`
}

// ghKey is a public SSH key of a GitHub user.
type ghKey struct {
	ID  int64  `json:"id"`
	Key string `json:"key"`
}

// ghIssue is an Issue as created or read.
type ghIssue struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

// ghComment is an Issue comment with its author.
type ghComment struct {
	Body string   `json:"body"`
	User ghAuthor `json:"user"`
}

// ghAuthor is a comment's author; Type is User or Bot.
type ghAuthor struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func jsonBody(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, Errorf(CodeGitHubAPIFailed, "encode the request: %v", err)
	}
	return bytes.NewReader(b), nil
}

func (g restGitHub) RepoPermissions(ctx context.Context, repo string) (repoPerms, error) {
	var r struct {
		Permissions repoPerms `json:"permissions"`
		Private     bool      `json:"private"`
	}
	err := g.c.DoWithContext(ctx, http.MethodGet, repoPath(repo), nil, &r)
	if notFound(err) {
		// GitHub hides a repository the login cannot see: it has no
		// permissions there.
		return repoPerms{}, nil
	}
	r.Permissions.Private = r.Private
	return r.Permissions, apiError("read the permissions on "+repo, err)
}

func (g restGitHub) UserKeys(ctx context.Context, login string) ([]ghKey, error) {
	var all []ghKey
	for page := 1; ; page++ {
		var keys []ghKey
		p := fmt.Sprintf("users/%s/keys?per_page=100&page=%d", url.PathEscape(login), page)
		if err := g.c.DoWithContext(ctx, http.MethodGet, p, nil, &keys); err != nil {
			return nil, apiError("list the SSH keys of "+login, err)
		}
		all = append(all, keys...)
		if len(keys) < 100 {
			return all, nil
		}
	}
}

// CreateIssue opens an Issue once: after a failed send it re-reads the
// Issues updated since just before the first send and sends again only when
// none has this title and body (R4.38).
func (g restGitHub) CreateIssue(ctx context.Context, repo, title, body string) (ghIssue, error) {
	var is ghIssue
	in, err := json.Marshal(map[string]string{"title": title, "body": body})
	if err != nil {
		return is, Errorf(CodeGitHubAPIFailed, "encode the request: %v", err)
	}
	path := repoPath(repo) + "/issues"
	since := sinceParam(now())
	err = ghretry.Do(ctx, http.MethodPost, path, func(ctx context.Context) error {
		is = ghIssue{}
		return g.c.DoWithContext(ctx, http.MethodPost, path, bytes.NewReader(in), &is)
	}, func(ctx context.Context) (bool, error) {
		var found []struct {
			ghIssue
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		q := path + "?state=all&sort=created&direction=desc&per_page=100&since=" + since
		if err := g.c.DoWithContext(ctx, http.MethodGet, q, nil, &found); err != nil {
			return false, err
		}
		for _, f := range found {
			if f.Title == title && f.Body == body {
				is = f.ghIssue
				return true, nil
			}
		}
		return false, nil
	})
	if err == nil && is.Number == 0 {
		return is, Errorf(CodeGitHubAPIFailed, "open an Issue in %s: no Issue number returned", repo)
	}
	return is, apiError("open an Issue in "+repo, err)
}

func (g restGitHub) Issue(ctx context.Context, repo string, number int) (ghIssue, error) {
	var is ghIssue
	err := g.c.DoWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/issues/%d", repoPath(repo), number), nil, &is)
	return is, apiError(fmt.Sprintf("read %s#%d", repo, number), err)
}

func (g restGitHub) IssueComments(ctx context.Context, repo string, number int) ([]ghComment, error) {
	var all []ghComment
	for page := 1; ; page++ {
		var cs []ghComment
		p := fmt.Sprintf("%s/issues/%d/comments?per_page=100&page=%d", repoPath(repo), number, page)
		if err := g.c.DoWithContext(ctx, http.MethodGet, p, nil, &cs); err != nil {
			return nil, apiError(fmt.Sprintf("read the comments of %s#%d", repo, number), err)
		}
		all = append(all, cs...)
		if len(cs) < 100 {
			return all, nil
		}
	}
}

// CreateComment comments once: after a failed send it re-reads the comments
// updated since just before the first send and sends again only when none
// has this body (R4.38).
func (g restGitHub) CreateComment(ctx context.Context, repo string, number int, body string) error {
	in, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return Errorf(CodeGitHubAPIFailed, "encode the request: %v", err)
	}
	path := fmt.Sprintf("%s/issues/%d/comments", repoPath(repo), number)
	since := sinceParam(now())
	err = ghretry.Do(ctx, http.MethodPost, path, func(ctx context.Context) error {
		var out json.RawMessage
		return g.c.DoWithContext(ctx, http.MethodPost, path, bytes.NewReader(in), &out)
	}, func(ctx context.Context) (bool, error) {
		var found []ghComment
		if err := g.c.DoWithContext(ctx, http.MethodGet, path+"?per_page=100&since="+since, nil, &found); err != nil {
			return false, err
		}
		for _, f := range found {
			if f.Body == body {
				return true, nil
			}
		}
		return false, nil
	})
	return apiError(fmt.Sprintf("comment on %s#%d", repo, number), err)
}

// now is the clock of the re-read window; tests replace it.
var now = time.Now

// sinceParam is a since query value a minute before t, which allows for
// clock skew between this host and GitHub.
func sinceParam(t time.Time) string {
	return url.QueryEscape(t.Add(-time.Minute).UTC().Format(time.RFC3339))
}

// CloseIssue closes the Issue with reason, completed or not_planned.
func (g restGitHub) CloseIssue(ctx context.Context, repo string, number int, reason string) error {
	in, err := jsonBody(map[string]string{"state": "closed", "state_reason": reason})
	if err != nil {
		return err
	}
	var out json.RawMessage
	err = g.c.DoWithContext(ctx, http.MethodPatch, fmt.Sprintf("%s/issues/%d", repoPath(repo), number), in, &out)
	return apiError(fmt.Sprintf("close %s#%d", repo, number), err)
}

// runnerRelease is the latest actions/runner release.
type runnerRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

// runnerReleaseURL is the public latest release of the runner; tests replace it.
var runnerReleaseURL = "https://api.github.com/repos/actions/runner/releases/latest"

// releaseDownload picks the linux-<arch> archive of the latest runner
// release, read through the credential-free httpClient. Its digest must be
// a sha256, which downloadArchive then checks.
func releaseDownload(ctx context.Context, arch string) (runnerDownload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runnerReleaseURL, nil)
	if err != nil {
		return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "read the latest runner release: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return runnerDownload{}, Errorf(CodeCancelled, "cancelled")
		}
		return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "read the latest runner release: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "read the latest runner release: HTTP %d", resp.StatusCode)
	}
	var rel runnerRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "read the latest runner release: %v", err)
	}
	version := strings.TrimPrefix(rel.TagName, "v")
	name := "actions-runner-linux-" + arch + "-" + version + ".tar.gz"
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		sum, ok := strings.CutPrefix(a.Digest, "sha256:")
		if !ok || !sha256Hex.MatchString(sum) {
			return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "the runner release asset %s has no sha256 digest", name)
		}
		return runnerDownload{OS: "linux", Architecture: arch, DownloadURL: a.BrowserDownloadURL, Filename: name, SHA256: sum}, nil
	}
	return runnerDownload{}, Errorf(CodeRunnerDownloadFailed, "the runner release %q has no asset %s", rel.TagName, name)
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
