package op

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

// github is the part of the GitHub REST API the runner commands use. Repositories
// are HOST/OWNER/REPO; the client acts with the current gh login of that
// host, whose token only go-gh handles.
type github interface {
	// User returns the id and login of the authenticated user.
	User(ctx context.Context) (ghUser, error)
	// RepoPermissions returns the gh login's permissions on repo; none
	// for a repository the login cannot see.
	RepoPermissions(ctx context.Context, repo string) (repoPerms, error)
	// UserKeys lists the verified public SSH keys of login.
	UserKeys(ctx context.Context, login string) ([]ghKey, error)
	CreateIssue(ctx context.Context, repo, title, body string) (ghIssue, error)
	Issue(ctx context.Context, repo string, number int) (ghIssue, error)
	IssueComments(ctx context.Context, repo string, number int) ([]ghComment, error)
	CreateComment(ctx context.Context, repo string, number int, body string) error
	CloseIssue(ctx context.Context, repo string, number int, reason string) error
	Downloads(ctx context.Context, repo string) ([]runnerDownload, error)
	// RegistrationToken returns a token that registers one runner; it
	// expires after an hour.
	RegistrationToken(ctx context.Context, repo string) (string, error)
	ListRunners(ctx context.Context, repo string) ([]ghRunner, error)
	RemoveCustomLabels(ctx context.Context, repo string, id int64) error
	// SetCustomLabels replaces every custom label of the runner id with
	// labels; it needs admin of repo.
	SetCustomLabels(ctx context.Context, repo string, id int64, labels []string) error
	DeleteRunner(ctx context.Context, repo string, id int64) error
}

type runnerDownload struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	DownloadURL  string `json:"download_url"`
	Filename     string `json:"filename"`
	SHA256       string `json:"sha256_checksum"`
}

type ghRunner struct {
	ID     int64     `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	Busy   bool      `json:"busy"`
	Labels []ghLabel `json:"labels"`
}

// ghLabel is a runner label; Type is "read-only" or "custom".
type ghLabel struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// hasCustomLabel reports whether the runner carries the custom label name.
func (g ghRunner) hasCustomLabel(name string) bool {
	for _, l := range g.Labels {
		if l.Type == "custom" && l.Name == name {
			return true
		}
	}
	return false
}

// customLabels reports whether the runner carries any custom label.
func (g ghRunner) customLabels() bool {
	for _, l := range g.Labels {
		if l.Type == "custom" {
			return true
		}
	}
	return false
}

// newGitHub returns the client for a host; tests replace it.
var newGitHub = func(host string) (github, error) {
	c, err := api.NewRESTClient(api.ClientOptions{Host: host})
	if err != nil {
		return nil, Errorf(CodeGitHubAPIFailed, "cannot use the gh login for %s: %v", host, err)
	}
	return restGitHub{c}, nil
}

type restGitHub struct{ c *api.RESTClient }

// repoPath turns HOST/OWNER/REPO into the API path repos/OWNER/REPO.
func repoPath(repo string) string {
	_, rest, _ := strings.Cut(repo, "/")
	return "repos/" + rest
}

func repoHost(repo string) string {
	h, _, _ := strings.Cut(repo, "/")
	return h
}

func apiError(what string, err error) error {
	if err == nil {
		return nil
	}
	var h *api.HTTPError
	if errors.As(err, &h) {
		msg := h.Message
		if msg == "" {
			msg = http.StatusText(h.StatusCode)
		}
		return Errorf(CodeGitHubAPIFailed, "%s: HTTP %d: %s", what, h.StatusCode, msg)
	}
	return Errorf(CodeGitHubAPIFailed, "%s: %v", what, err)
}

func (g restGitHub) User(ctx context.Context) (ghUser, error) {
	var u ghUser
	if err := g.c.DoWithContext(ctx, http.MethodGet, "user", nil, &u); err != nil {
		return u, apiError("get the gh login's user", err)
	}
	if u.ID == 0 || u.Login == "" {
		return u, Errorf(CodeGitHubAPIFailed, "get the gh login's user: no user id or login")
	}
	return u, nil
}

func (g restGitHub) Downloads(ctx context.Context, repo string) ([]runnerDownload, error) {
	var d []runnerDownload
	err := g.c.DoWithContext(ctx, http.MethodGet, repoPath(repo)+"/actions/runners/downloads", nil, &d)
	return d, apiError("list the runner downloads of "+repo, err)
}

func (g restGitHub) RegistrationToken(ctx context.Context, repo string) (string, error) {
	var t struct {
		Token string `json:"token"`
	}
	if err := g.c.DoWithContext(ctx, http.MethodPost, repoPath(repo)+"/actions/runners/registration-token", nil, &t); err != nil {
		return "", apiError("create a runner registration token for "+repo, err)
	}
	if t.Token == "" {
		return "", Errorf(CodeGitHubAPIFailed, "create a runner registration token for %s: no token returned", repo)
	}
	return t.Token, nil
}

func (g restGitHub) ListRunners(ctx context.Context, repo string) ([]ghRunner, error) {
	var all []ghRunner
	for page := 1; ; page++ {
		var r struct {
			Total   int        `json:"total_count"`
			Runners []ghRunner `json:"runners"`
		}
		p := fmt.Sprintf("%s/actions/runners?per_page=100&page=%d", repoPath(repo), page)
		if err := g.c.DoWithContext(ctx, http.MethodGet, p, nil, &r); err != nil {
			return nil, apiError("list the runners of "+repo, err)
		}
		all = append(all, r.Runners...)
		if len(r.Runners) < 100 || len(all) >= r.Total {
			return all, nil
		}
	}
}

// RemoveCustomLabels removes every custom label of the runner id; GitHub
// keeps its read-only labels. A runner that is gone already is success.
func (g restGitHub) RemoveCustomLabels(ctx context.Context, repo string, id int64) error {
	var out json.RawMessage
	err := g.c.DoWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/actions/runners/%d/labels", repoPath(repo), id), nil, &out)
	if notFound(err) {
		return nil
	}
	return apiError(fmt.Sprintf("remove the custom labels of runner %d in %s", id, repo), err)
}

// SetCustomLabels replaces every custom label of the runner id with labels
// (PUT .../labels); GitHub keeps its read-only labels.
func (g restGitHub) SetCustomLabels(ctx context.Context, repo string, id int64, labels []string) error {
	body, err := json.Marshal(map[string][]string{"labels": labels})
	if err != nil {
		return Errorf(CodeGitHubAPIFailed, "set the labels of runner %d in %s: %v", id, repo, err)
	}
	var out json.RawMessage
	err = g.c.DoWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/actions/runners/%d/labels", repoPath(repo), id), bytes.NewReader(body), &out)
	return apiError(fmt.Sprintf("set the labels of runner %d in %s", id, repo), err)
}

// DeleteRunner deletes the registration of the runner id. A runner that is
// gone already is success.
func (g restGitHub) DeleteRunner(ctx context.Context, repo string, id int64) error {
	err := g.c.DoWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/actions/runners/%d", repoPath(repo), id), nil, nil)
	if notFound(err) {
		return nil
	}
	return apiError(fmt.Sprintf("delete runner %d from %s", id, repo), err)
}

func notFound(err error) bool {
	var h *api.HTTPError
	return errors.As(err, &h) && h.StatusCode == http.StatusNotFound
}

// httpClient downloads the runner archive. It sends no credentials and
// follows only https redirects; tests replace it.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to %s is not https", req.URL.Redacted())
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// downloadArchive fetches d into a temporary file and verifies its checksum
// as it goes. The returned cleanup removes the file.
func downloadArchive(ctx context.Context, d runnerDownload) (string, func(), error) {
	u, err := url.Parse(d.DownloadURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", nil, Errorf(CodeRunnerDownloadFailed, "the runner download URL %q is not https", d.DownloadURL)
	}
	want := strings.ToLower(strings.TrimSpace(d.SHA256))
	if len(want) != sha256.Size*2 {
		return "", nil, Errorf(CodeRunnerDownloadFailed, "the runner download %s has no sha256 checksum", d.Filename)
	}
	f, err := os.CreateTemp("", "nibfin-runner-*.tar.gz")
	if err != nil {
		return "", nil, Errorf(CodeRunnerDownloadFailed, "cannot create a temporary file: %v", err)
	}
	cleanup := func() {
		f.Close()
		os.Remove(f.Name())
	}
	fail := func(format string, args ...any) (string, func(), error) {
		cleanup()
		return "", nil, Errorf(CodeRunnerDownloadFailed, format, args...)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fail("download %s: %v", d.Filename, err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			cleanup()
			return "", nil, Errorf(CodeCancelled, "cancelled")
		}
		return fail("download %s: %v", d.Filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail("download %s: HTTP %d", d.Filename, resp.StatusCode)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return fail("download %s: %v", d.Filename, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fail("download %s: sha256 %s does not match the published %s", d.Filename, got, want)
	}
	if err := f.Close(); err != nil {
		return fail("download %s: %v", d.Filename, err)
	}
	return f.Name(), cleanup, nil
}
