package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
)

// graphQL is the part of go-gh's GraphQL client the label operations use.
type graphQL interface {
	DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error
}

// newGraphQL returns a client for host using the current gh login. go-gh
// resolves the token; this package never sees it. Tests replace it.
var newGraphQL = func(host string) (graphQL, error) {
	return api.NewGraphQLClient(api.ClientOptions{Host: host, Transport: ghretry.Transport(nil)})
}

// labelOp adds or removes labels on Issues: one query to plan, one mutation
// to apply, one query to read back.
type labelOp struct {
	name   string   // operation name
	add    string   // label to add, or ""
	remove []string // labels to remove
	done   string   // result value per Issue
	params IssuesParams

	// Resolved by Plan.
	planned bool
	client  graphQL
	issues  []issueState
}

type issueState struct {
	ref      issueRef
	id, url  string
	labelIDs []string // labels to add or remove; empty when nothing changes
	names    []string // the names of those labels
	current  string   // the relevant labels at plan time
}

type labelNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type issueNode struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Labels struct {
		Nodes []labelNode `json:"nodes"`
	} `json:"labels"`
}

func hasLabel(labels []labelNode, name string) bool {
	for _, l := range labels {
		if strings.EqualFold(l.Name, name) {
			return true
		}
	}
	return false
}

func labelNames(labels []labelNode) string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = l.Name
	}
	return "[" + strings.Join(names, ", ") + "]"
}

// Plan reads every repository and Issue in one query and lists the Issues
// that need a change.
func (o *labelOp) Plan(ctx context.Context) (Plan, error) {
	p := Plan{Operation: o.name}
	refs, err := parseIssueRefs(o.params.Repo, o.params.Issues)
	if err != nil {
		return p, err
	}
	client, err := newGraphQL(refs[0].Host)
	if err != nil {
		return p, Errorf(CodeAuthUnavailable, "no gh login for %s: %w", refs[0].Host, err)
	}

	// One aliased repository field per distinct repository, one aliased
	// issue field per reference.
	var repoKeys []string
	repoAlias := map[string]string{}
	var q, decl strings.Builder
	vars := map[string]any{}
	issuesOf := map[string][]int{}
	for i, r := range refs {
		k := r.repoKey()
		if _, ok := repoAlias[k]; !ok {
			repoAlias[k] = "r" + strconv.Itoa(len(repoKeys))
			repoKeys = append(repoKeys, k)
		}
		issuesOf[k] = append(issuesOf[k], i)
	}
	for _, k := range repoKeys {
		a := repoAlias[k]
		r := refs[issuesOf[k][0]]
		fmt.Fprintf(&decl, "$%so: String!, $%sn: String!, ", a, a)
		vars[a+"o"], vars[a+"n"] = r.Owner, r.Name
		fmt.Fprintf(&q, "%s: repository(owner: $%so, name: $%sn) {", a, a, a)
		if o.add != "" {
			fmt.Fprintf(&q, " label: label(name: %q) { id name }", o.add)
		}
		for _, i := range issuesOf[k] {
			fmt.Fprintf(&q, " i%d: issue(number: %d) { id url labels(first: 100) { nodes { id name } } }", i, refs[i].Number)
		}
		q.WriteString(" } ")
	}
	query := "query(" + strings.TrimSuffix(decl.String(), ", ") + ") { " + q.String() + "}"

	var data map[string]map[string]json.RawMessage
	err = client.DoWithContext(ctx, query, vars, &data)
	notFound := map[string]bool{} // aliases GitHub reported as NOT_FOUND
	if err != nil {
		var gqlErr *api.GraphQLError
		if !errors.As(err, &gqlErr) {
			return p, Errorf(CodeGitHubError, "%s: %w", o.name, err)
		}
		for _, e := range gqlErr.Errors {
			path := PathAliases(e.Path)
			if e.Type != "NOT_FOUND" || len(path) == 0 {
				return p, Errorf(CodeGitHubError, "%s: %w", o.name, err)
			}
			notFound[strings.Join(path, ".")] = true
		}
	}

	var missing, noLabel []string
	o.issues = nil
	for _, k := range repoKeys {
		a := repoAlias[k]
		repoData := data[a]
		if repoData == nil || notFound[a] {
			for _, i := range issuesOf[k] {
				missing = append(missing, refs[i].Input)
			}
			continue
		}
		var label *labelNode
		if o.add != "" && !notFound[a+".label"] {
			if err := decode(repoData["label"], &label); err != nil {
				return p, Errorf(CodeGitHubError, "%s: %w", o.name, err)
			}
		}
		for _, i := range issuesOf[k] {
			alias := "i" + strconv.Itoa(i)
			var n *issueNode
			if !notFound[a+"."+alias] {
				if err := decode(repoData[alias], &n); err != nil {
					return p, Errorf(CodeGitHubError, "%s: %w", o.name, err)
				}
			}
			if n == nil || n.ID == "" {
				missing = append(missing, refs[i].Input)
				continue
			}
			s := issueState{ref: refs[i], id: n.ID, url: n.URL, current: o.state(n.Labels.Nodes)}
			if o.add != "" {
				if label == nil || label.ID == "" {
					if len(noLabel) == 0 || noLabel[len(noLabel)-1] != refs[i].Owner+"/"+refs[i].Name {
						noLabel = append(noLabel, refs[i].Owner+"/"+refs[i].Name)
					}
				} else if !hasLabel(n.Labels.Nodes, o.add) {
					s.labelIDs, s.names = []string{label.ID}, []string{label.Name}
				}
			} else {
				// Remove only the labels the Issue carries.
				for _, l := range n.Labels.Nodes {
					for _, name := range o.remove {
						if strings.EqualFold(l.Name, name) {
							s.labelIDs, s.names = append(s.labelIDs, l.ID), append(s.names, l.Name)
						}
					}
				}
			}
			o.issues = append(o.issues, s)
		}
	}
	if len(missing) > 0 {
		o.issues = nil
		return p, Errorf(CodeNotFound, "not found: %s", strings.Join(missing, ", "))
	}
	if len(noLabel) > 0 {
		o.issues = nil
		return p, Errorf(CodeLabelMissing, "no %q label in %s; create it first", o.add, strings.Join(noLabel, ", "))
	}

	for _, k := range repoKeys {
		r := refs[issuesOf[k][0]]
		if p.Repo != "" {
			p.Repo += ", "
		}
		p.Repo += r.Host + "/" + r.Owner + "/" + r.Name
	}
	p.Steps = []Step{}
	for _, s := range o.issues {
		p.Steps = append(p.Steps, o.step(s))
	}
	o.client, o.planned = client, true
	return p, nil
}

// step is the plan item of one Issue; an Issue already in the target state
// is a no-change item.
func (o *labelOp) step(s issueState) Step {
	st := Step{ID: "issue:" + s.url, Action: "remove label", Target: s.url, Current: s.current, Next: o.target()}
	if o.add != "" {
		st.Action = "add label"
	}
	if len(s.labelIDs) == 0 {
		st.NoChange, st.Detail = true, "already "+o.done
	} else {
		st.Detail = strings.Join(s.names, ", ")
	}
	return st
}

// relevant are the labels the operation adds or removes.
func (o *labelOp) relevant() []string {
	if o.add != "" {
		return []string{o.add}
	}
	return o.remove
}

// state names the relevant labels an Issue carries, or "no <labels>".
func (o *labelOp) state(labels []labelNode) string {
	var has []string
	for _, name := range o.relevant() {
		if hasLabel(labels, name) {
			has = append(has, name)
		}
	}
	if len(has) == 0 {
		return "no " + strings.Join(o.relevant(), " or ")
	}
	return strings.Join(has, ", ")
}

// target is the state of an Issue once the operation applied.
func (o *labelOp) target() string {
	if o.add != "" {
		return o.add
	}
	return "no " + strings.Join(o.remove, " or ")
}

// Apply sends one mutation with one field per Issue that needs a change.
// Nothing is sent when no Issue does. Each changing Issue's progress follows
// the mutation: done only on a successful response; on an error the Issues
// its path names fail with it, and the others fail as unknown.
func (o *labelOp) Apply(ctx context.Context, _ Plan) error {
	if !o.planned {
		return fmt.Errorf("%s: apply before plan", o.name)
	}
	field := "addLabelsToLabelable"
	if o.add == "" {
		field = "removeLabelsFromLabelable"
	}
	var q, decl strings.Builder
	vars := map[string]any{}
	alias := map[string]Step{}
	var changing []Step
	var keys []string
	for i, s := range o.issues {
		if len(s.labelIDs) == 0 {
			continue
		}
		a := "m" + strconv.Itoa(i)
		st := o.step(s)
		alias[a] = st
		keys = append(keys, a)
		changing = append(changing, st)
		fmt.Fprintf(&decl, "$%si: ID!, $%sl: [ID!]!, ", a, a)
		vars[a+"i"], vars[a+"l"] = s.id, s.labelIDs
		fmt.Fprintf(&q, "%s: %s(input: {labelableId: $%si, labelIds: $%sl}) { clientMutationId } ", a, field, a, a)
	}
	if len(vars) == 0 {
		return nil
	}
	b := StartAll(ctx, changing)
	mutation := "mutation(" + strings.TrimSuffix(decl.String(), ", ") + ") { " + q.String() + "}"
	var data map[string]json.RawMessage
	if err := o.client.DoWithContext(ctx, mutation, vars, &data); err != nil {
		var failed []string
		named := map[string]bool{}
		var gqlErr *api.GraphQLError
		if errors.As(err, &gqlErr) {
			for _, e := range gqlErr.Errors {
				if p := PathAliases(e.Path); len(p) > 0 && alias[p[0]].ID != "" && !named[p[0]] {
					named[p[0]] = true
					failed = append(failed, alias[p[0]].Target)
				}
			}
		}
		var out error
		if len(failed) > 0 {
			out = Errorf(CodeGitHubError, "%s failed for %s: %w", o.name, strings.Join(failed, ", "), err)
		} else {
			out = Errorf(CodeGitHubError, "%s: %w", o.name, err)
		}
		for _, a := range keys {
			if named[a] {
				b.Fail(alias[a].ID, out)
			}
		}
		b.Close(Errorf(CodeGitHubError, "the %s mutation failed, so the outcome for this Issue is unknown: %v", o.name, err))
		return out
	}
	b.Close(nil)
	return nil
}

// ReadBack reads every planned Issue's labels in one query and checks they
// match the target. Every Issue is an item, including those that needed no
// change; the items are returned also with an error.
func (o *labelOp) ReadBack(ctx context.Context, _ Plan) (Result, error) {
	if !o.planned {
		return nil, fmt.Errorf("%s: read back before plan", o.name)
	}
	var checks Checks
	ids := make([]string, len(o.issues))
	for i, s := range o.issues {
		ids[i] = s.id
		checks.Expect(o.step(s))
	}
	const query = "query($ids: [ID!]!) { nodes(ids: $ids) { ... on Issue { id url labels(first: 100) { nodes { id name } } } } }"
	var data struct {
		Nodes []*issueNode `json:"nodes"`
	}
	if err := o.client.DoWithContext(ctx, query, map[string]any{"ids": ids}, &data); err != nil {
		err = Errorf(CodeGitHubError, "%s read-back: %w", o.name, err)
		return checks.Fill(nil, nil, err), err
	}
	byID := map[string]*issueNode{}
	for _, n := range data.Nodes {
		if n != nil {
			byID[n.ID] = n
		}
	}
	r := Result{}
	var wrong []string
	for _, s := range o.issues {
		n := byID[s.id]
		st := o.step(s)
		switch {
		case n == nil:
			wrong = append(wrong, s.url+" (not read back)")
			checks.Set(st, false, "", "not read back")
		case !o.reached(n.Labels.Nodes):
			wrong = append(wrong, s.url+" has "+labelNames(n.Labels.Nodes))
			checks.Set(st, false, o.state(n.Labels.Nodes), "has "+labelNames(n.Labels.Nodes))
		default:
			r[s.url] = o.done
			checks.Set(st, true, o.state(n.Labels.Nodes), "")
		}
	}
	checks.Fill(r, nil, nil)
	if len(wrong) > 0 {
		return r, Errorf(CodeNotApplied, "%s not applied: %s", o.name, strings.Join(wrong, "; "))
	}
	return r, nil
}

// reached reports whether labels are in the operation's target state.
func (o *labelOp) reached(labels []labelNode) bool {
	if o.add != "" {
		return hasLabel(labels, o.add)
	}
	for _, name := range o.remove {
		if hasLabel(labels, name) {
			return false
		}
	}
	return true
}

// PathAliases returns the leading string elements of a GraphQL error path:
// the aliases and field names before the first list index.
func PathAliases(path []any) []string {
	var out []string
	for _, p := range path {
		s, ok := p.(string)
		if !ok {
			break
		}
		out = append(out, s)
	}
	return out
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}
