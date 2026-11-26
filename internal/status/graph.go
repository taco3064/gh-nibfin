package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
	"github.com/taco3064/gh-nibfin/internal/op"
)

// graphQL is the part of go-gh's GraphQL client status uses.
type graphQL interface {
	DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error
}

// newGraphQL returns a GraphQL client for host that carries the current gh
// login; only go-gh handles its token. Tests replace it.
var newGraphQL = func(host string) (graphQL, error) {
	return api.NewGraphQLClient(api.ClientOptions{Host: host, Transport: ghretry.Transport(nil)})
}

const (
	// issuesPerQuery bounds each blocker query: 50 Issues of 100 blockers
	// stay far below GitHub's node limit.
	issuesPerQuery = 50

	nibfinWorkflow = ".github/workflows/nibfin-issue.yml"

	blockedFields = `pageInfo { hasNextPage endCursor } nodes { number title state url repository { nameWithOwner } }`
)

// repoGroup is one repository's listed Issues and, once read, the current
// gh account's permission on it.
type repoGroup struct {
	repo    string // OWNER/REPO on the reader's host
	issues  []*Issue
	perm    *string
	permErr *op.Error
}

func newGroup(repo string, issues []Issue) *repoGroup {
	g := &repoGroup{repo: repo}
	for i := range issues {
		g.issues = append(g.issues, &issues[i])
	}
	return g
}

// links reads the blockers and permission, then the runs, of the groups.
func (r *reader) links(ctx context.Context, groups []*repoGroup) {
	r.issueGraph(ctx, groups)
	r.issueRuns(ctx, groups)
}

// graph returns the GraphQL client for the reader's host.
func (r *reader) graph() (graphQL, *op.Error) {
	if r.gql == nil && r.gqlErr == nil {
		c, err := newGraphQL(r.host)
		if err != nil {
			r.gqlErr = op.AsError(op.Errorf(op.CodeAuthUnavailable, "cannot use the gh login for %s: %v", r.host, err))
		} else {
			r.gql = c
		}
	}
	return r.gql, r.gqlErr
}

type blockedConn struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []*struct {
		Number     int    `json:"number"`
		Title      string `json:"title"`
		State      string `json:"state"`
		URL        string `json:"url"`
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
	} `json:"nodes"`
}

func (c *blockedConn) add(is *Issue) {
	for _, n := range c.Nodes {
		if n != nil {
			is.BlockedBy = append(is.BlockedBy, Blocker{Repo: n.Repository.NameWithOwner, Number: n.Number, Title: n.Title, State: n.State, URL: n.URL})
		}
	}
}

// graphPart is one repository alias of a blocker query: a group and the
// Issues of it the query reads.
type graphPart struct {
	g      *repoGroup
	issues []*Issue
}

// issueGraph reads every group's permission and its Issues' blockers in
// batched queries of at most issuesPerQuery Issues.
func (r *reader) issueGraph(ctx context.Context, groups []*repoGroup) {
	var queries [][]graphPart
	var cur []graphPart
	n := 0
	flush := func() {
		if len(cur) > 0 {
			queries = append(queries, cur)
		}
		cur, n = nil, 0
	}
	for _, g := range groups {
		if len(g.issues) == 0 {
			cur = append(cur, graphPart{g: g})
			continue
		}
		for rest := g.issues; len(rest) > 0; {
			if n == issuesPerQuery {
				flush()
			}
			k := min(issuesPerQuery-n, len(rest))
			cur = append(cur, graphPart{g: g, issues: rest[:k]})
			rest, n = rest[k:], n+k
		}
	}
	flush()
	cursors := map[*Issue]string{}
	for _, q := range queries {
		r.graphQuery(ctx, q, cursors)
	}
	for _, g := range groups {
		for _, is := range g.issues {
			if is.BlockedByError == nil {
				r.morePages(ctx, g.repo, is, cursors[is])
			}
		}
	}
}

func setPerm(g *repoGroup, e *op.Error) {
	if g.perm == nil && g.permErr == nil {
		g.permErr = e
	}
}

func setBlockedErr(is *Issue, e *op.Error) {
	if is.BlockedByError == nil {
		is.BlockedByError = e
	}
}

// graphQuery sends one blocker query. Its errors are the parts' errors: an
// error path names the Issue or repository alias it is about.
func (r *reader) graphQuery(ctx context.Context, parts []graphPart, cursors map[*Issue]string) {
	var q, decl strings.Builder
	vars := map[string]any{}
	names := []string{}
	for k, p := range parts {
		a := "r" + strconv.Itoa(k)
		owner, name, _ := strings.Cut(p.g.repo, "/")
		fmt.Fprintf(&decl, "$%so: String!, $%sn: String!, ", a, a)
		vars[a+"o"], vars[a+"n"] = owner, name
		fmt.Fprintf(&q, "%s: repository(owner: $%so, name: $%sn) { viewerPermission", a, a, a)
		for j, is := range p.issues {
			fmt.Fprintf(&q, " i%d: issue(number: %d) { blockedBy(first: 100) { %s } }", j, is.Number, blockedFields)
		}
		q.WriteString(" } ")
		names = append(names, p.g.repo)
	}
	query := "query(" + strings.TrimSuffix(decl.String(), ", ") + ") { " + q.String() + "}"

	fail := func(e *op.Error) {
		for _, p := range parts {
			setPerm(p.g, e)
			for _, is := range p.issues {
				setBlockedErr(is, e)
			}
		}
	}
	c, gerr := r.graph()
	if gerr != nil {
		fail(gerr)
		return
	}
	var data map[string]map[string]json.RawMessage
	err := c.DoWithContext(ctx, query, vars, &data)
	if err != nil {
		var gqlErr *api.GraphQLError
		if !errors.As(err, &gqlErr) {
			fail(gqlFail("read the blockers and permission of "+strings.Join(names, ", "), err))
			return
		}
		for _, item := range gqlErr.Errors {
			path := op.PathAliases(item.Path)
			k := -1
			if len(path) > 0 && strings.HasPrefix(path[0], "r") {
				if v, err := strconv.Atoi(path[0][1:]); err == nil && v < len(parts) {
					k = v
				}
			}
			if k < 0 {
				fail(gqlFail("read the blockers and permission of "+strings.Join(names, ", "), errors.New(item.Message)))
				continue
			}
			p := parts[k]
			if len(path) >= 2 && strings.HasPrefix(path[1], "i") {
				if j, err := strconv.Atoi(path[1][1:]); err == nil && j < len(p.issues) {
					is := p.issues[j]
					setBlockedErr(is, gqlFail(fmt.Sprintf("read the blockers of %s#%d", p.g.repo, is.Number), errors.New(item.Message)))
					continue
				}
			}
			e := gqlFail("read "+p.g.repo, errors.New(item.Message))
			setPerm(p.g, e)
			if len(path) < 2 || path[1] != "viewerPermission" {
				for _, is := range p.issues {
					setBlockedErr(is, e)
				}
			}
		}
	}
	for k, p := range parts {
		repoData := data["r"+strconv.Itoa(k)]
		if repoData == nil {
			fail := gqlFail("read "+p.g.repo, errors.New("not returned"))
			setPerm(p.g, fail)
			for _, is := range p.issues {
				setBlockedErr(is, fail)
			}
			continue
		}
		if p.g.permErr == nil {
			var perm *string
			if err := decode(repoData["viewerPermission"], &perm); err != nil {
				setPerm(p.g, gqlFail("read the permission on "+p.g.repo, err))
			} else if perm != nil {
				p.g.perm = perm
			}
		}
		for j, is := range p.issues {
			if is.BlockedByError != nil {
				continue
			}
			var node *struct {
				BlockedBy *blockedConn `json:"blockedBy"`
			}
			err := decode(repoData["i"+strconv.Itoa(j)], &node)
			switch {
			case err != nil:
				setBlockedErr(is, gqlFail(fmt.Sprintf("read the blockers of %s#%d", p.g.repo, is.Number), err))
			case node == nil || node.BlockedBy == nil:
				setBlockedErr(is, gqlFail(fmt.Sprintf("read the blockers of %s#%d", p.g.repo, is.Number), errors.New("not returned")))
			default:
				node.BlockedBy.add(is)
				if node.BlockedBy.PageInfo.HasNextPage {
					cursors[is] = node.BlockedBy.PageInfo.EndCursor
				}
			}
		}
	}
}

// morePages reads the blockers past the first 100 of one Issue.
func (r *reader) morePages(ctx context.Context, repo string, is *Issue, cursor string) {
	owner, name, _ := strings.Cut(repo, "/")
	for cursor != "" {
		c, gerr := r.graph()
		if gerr != nil {
			is.BlockedByError = gerr
			return
		}
		query := fmt.Sprintf("query($o: String!, $n: String!, $c: String!) { repository(owner: $o, name: $n) { issue(number: %d) { blockedBy(first: 100, after: $c) { %s } } } }", is.Number, blockedFields)
		var data struct {
			Repository *struct {
				Issue *struct {
					BlockedBy *blockedConn `json:"blockedBy"`
				} `json:"issue"`
			} `json:"repository"`
		}
		err := c.DoWithContext(ctx, query, map[string]any{"o": owner, "n": name, "c": cursor}, &data)
		cursor = ""
		what := fmt.Sprintf("read the blockers of %s#%d", repo, is.Number)
		if err != nil {
			is.BlockedByError = gqlFail(what, err)
			return
		}
		if data.Repository == nil || data.Repository.Issue == nil || data.Repository.Issue.BlockedBy == nil {
			is.BlockedByError = gqlFail(what, errors.New("not returned"))
			return
		}
		conn := data.Repository.Issue.BlockedBy
		conn.add(is)
		if conn.PageInfo.HasNextPage {
			cursor = conn.PageInfo.EndCursor
		}
	}
}

// gqlFail keeps GitHub's message; a token never appears in it.
func gqlFail(what string, err error) *op.Error {
	return op.AsError(op.Errorf(op.CodeGitHubAPIFailed, "%s: %v", what, err))
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

const reposQuery = `query($after: String) { viewer { repositories(first: 100, after: $after, ` +
	`affiliations: [OWNER, COLLABORATOR, ORGANIZATION_MEMBER], ownerAffiliations: [OWNER, COLLABORATOR, ORGANIZATION_MEMBER]) { ` +
	`pageInfo { hasNextPage endCursor } nodes { nameWithOwner url visibility viewerPermission ` +
	`defaultBranchRef { target { ... on Commit { file(path: "` + nibfinWorkflow + `") { oid } } } } } } } }`

// nibfinRepos lists the current gh account's repositories, owned, joined
// as a collaborator or through an organization, whose default branch has
// nibfin-issue.yml, in one paginated query. Execution stays nil; the view
// sets it from the runner report (withExecution).
func (r *reader) nibfinRepos(ctx context.Context) ([]NibfinRepo, *op.Error) {
	c, gerr := r.graph()
	if gerr != nil {
		return []NibfinRepo{}, gerr
	}
	out := []NibfinRepo{}
	var after any
	for {
		var data struct {
			Viewer struct {
				Repositories struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []*struct {
						NameWithOwner    string  `json:"nameWithOwner"`
						URL              string  `json:"url"`
						Visibility       string  `json:"visibility"`
						ViewerPermission *string `json:"viewerPermission"`
						DefaultBranchRef *struct {
							Target *struct {
								File *struct {
									OID string `json:"oid"`
								} `json:"file"`
							} `json:"target"`
						} `json:"defaultBranchRef"`
					} `json:"nodes"`
				} `json:"repositories"`
			} `json:"viewer"`
		}
		if err := c.DoWithContext(ctx, reposQuery, map[string]any{"after": after}, &data); err != nil {
			var gqlErr *api.GraphQLError
			if !errors.As(err, &gqlErr) || !missingWorkflowOnly(gqlErr) {
				return []NibfinRepo{}, gqlFail("list your repositories that use Nibfin", err)
			}
		}
		rs := data.Viewer.Repositories
		for _, n := range rs.Nodes {
			if n == nil || n.DefaultBranchRef == nil || n.DefaultBranchRef.Target == nil || n.DefaultBranchRef.Target.File == nil {
				continue
			}
			out = append(out, NibfinRepo{Repo: n.NameWithOwner, URL: n.URL, Visibility: n.Visibility, Permission: n.ViewerPermission})
		}
		if !rs.PageInfo.HasNextPage || rs.PageInfo.EndCursor == "" {
			break
		}
		after = rs.PageInfo.EndCursor
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Repo) < strings.ToLower(out[j].Repo) })
	return out, nil
}

// missingFilePath is the error path GitHub gives a repository list node
// whose default branch has no nibfin-issue.yml; "" stands for the node
// index, a JSON number.
var missingFilePath = []string{"viewer", "repositories", "nodes", "", "defaultBranchRef", "target", "file"}

// missingWorkflowOnly reports whether every error of a repository list
// page is GitHub's answer for a repository whose default branch has no
// nibfin-issue.yml. Such a repository does not use Nibfin; its node comes
// back with a null file next to the rest of the page.
func missingWorkflowOnly(e *api.GraphQLError) bool {
	if len(e.Errors) == 0 {
		return false
	}
	for _, item := range e.Errors {
		if !strings.HasPrefix(item.Message, "Could not resolve file for path") || len(item.Path) != len(missingFilePath) {
			return false
		}
		for i, want := range missingFilePath {
			if want == "" {
				switch item.Path[i].(type) {
				case float64, int:
					continue
				}
				return false
			}
			if s, ok := item.Path[i].(string); !ok || s != want {
				return false
			}
		}
	}
	return true
}

// execRepo is the execution repository, HOST/OWNER/REPO of this host's
// Runners, "" when none is configured; false when it cannot be read.
func (r *reader) execRepo() (string, bool) {
	if r.runners.Error != nil {
		return "", false
	}
	return r.runners.Repo, true
}
