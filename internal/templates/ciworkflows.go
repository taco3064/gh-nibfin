package templates

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"go.yaml.in/yaml/v3"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// RESTClient is the part of go-gh's api.RESTClient CIWorkflows uses. The client
// carries the current gh login; the token never leaves it.
type RESTClient interface {
	DoWithContext(ctx context.Context, method string, path string, body io.Reader, response any) error
}

// ciEvents are the triggers of a workflow that runs on push or can produce a
// required check.
var ciEvents = map[string]bool{
	"push":                true,
	"pull_request":        true,
	"pull_request_target": true,
	"merge_group":         true,
}

const perPage = 100

type workflowList struct {
	TotalCount int `json:"total_count"`
	Workflows  []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		State string `json:"state"`
	} `json:"workflows"`
}

type fileContent struct {
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// CIWorkflows returns the names of repo's CI/CD workflows: the active
// workflows under .github/workflows/, other than Nibfin's own nibfin-* files,
// whose default-branch file is triggered by push, pull_request,
// pull_request_target or merge_group. A listed workflow whose file is not on
// the default branch (for example one that only ever ran from a since-deleted
// branch) is not a CI/CD workflow and is skipped. repo is HOST/OWNER/REPO;
// client must be a client for HOST.
func CIWorkflows(ctx context.Context, client RESTClient, repo string) ([]string, error) {
	base, err := repoBase(repo)
	if err != nil {
		return nil, err
	}

	var names []string
	seen := 0
	for page := 1; ; page++ {
		var list workflowList
		p := fmt.Sprintf("%s/actions/workflows?per_page=%d&page=%d", base, perPage, page)
		if err := client.DoWithContext(ctx, http.MethodGet, p, nil, &list); err != nil {
			return nil, op.Wrap(op.CodeWorkflowsUnreadable, fmt.Errorf("list the workflows of %s: %w", repo, err))
		}
		for _, w := range list.Workflows {
			if w.State != "active" || !candidate(w.Path) {
				continue
			}
			ok, err := triggersCI(ctx, client, base, w.Path)
			if err != nil {
				return nil, op.Wrap(op.CodeWorkflowsUnreadable, fmt.Errorf("read %s of %s: %w", w.Path, repo, err))
			}
			if ok {
				names = append(names, w.Name)
			}
		}
		seen += len(list.Workflows)
		if len(list.Workflows) < perPage || seen >= list.TotalCount {
			return names, nil
		}
	}
}

// candidate reports whether a workflow path is a readable workflow file that
// is not one of Nibfin's own. Dynamic workflows have no file under
// .github/workflows/.
func candidate(p string) bool {
	dir, name := path.Split(p)
	return dir == Dir+"/" && name != "" && !strings.HasPrefix(name, "nibfin-")
}

// triggersCI reads the workflow file from the default branch and reports
// whether its on: has one of ciEvents. A file missing from the default branch
// gives false and no error; any other read or parse failure is returned.
func triggersCI(ctx context.Context, client RESTClient, base, p string) (bool, error) {
	raw, found, err := readFile(ctx, client, base, p, "")
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	events, err := triggers(raw)
	if err != nil {
		return false, err
	}
	for _, e := range events {
		if ciEvents[e] {
			return true, nil
		}
	}
	return false, nil
}

// ReadFile reads the file at path p of repo through the contents API. ref
// names a branch, tag or commit; empty means the default branch. A missing
// file gives found=false and no error. repo is HOST/OWNER/REPO; client must
// be a client for HOST.
func ReadFile(ctx context.Context, client RESTClient, repo, p, ref string) (data []byte, found bool, err error) {
	base, err := repoBase(repo)
	if err != nil {
		return nil, false, err
	}
	return readFile(ctx, client, base, p, ref)
}

func readFile(ctx context.Context, client RESTClient, base, p, ref string) ([]byte, bool, error) {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := base + "/contents/" + strings.Join(segs, "/")
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	var c fileContent
	if err := client.DoWithContext(ctx, http.MethodGet, u, nil, &c); err != nil {
		var h *api.HTTPError
		if errors.As(err, &h) && h.StatusCode == http.StatusNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	if c.Encoding != "base64" {
		return nil, false, fmt.Errorf("unexpected content encoding %q", c.Encoding)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(c.Content))
	if err != nil {
		return nil, false, fmt.Errorf("decode the content: %w", err)
	}
	return raw, true, nil
}

// repoBase turns HOST/OWNER/REPO into the REST path repos/OWNER/REPO.
func repoBase(repo string) (string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", op.Errorf(op.CodeInvalidRepo, "repository %q is not HOST/OWNER/REPO", repo)
	}
	return "repos/" + url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[2]), nil
}

// triggers returns the event names of a workflow's on:, which may be a
// string, a list or a map.
func triggers(raw []byte) ([]string, error) {
	var doc struct {
		On yaml.Node `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse the workflow: %w", err)
	}
	n := &doc.On
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.ScalarNode:
		return []string{n.Value}, nil
	case yaml.SequenceNode:
		var out []string
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("on: has a non-string list item at line %d", item.Line)
			}
			out = append(out, item.Value)
		}
		return out, nil
	case yaml.MappingNode:
		var out []string
		for i := 0; i < len(n.Content); i += 2 {
			out = append(out, n.Content[i].Value)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("on: at line %d is not a string, list or map", n.Line)
	}
}
