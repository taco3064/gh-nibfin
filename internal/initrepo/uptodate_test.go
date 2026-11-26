package initrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// fakeChecks is the required status checks of branch protection. The PATCH
// is the strict reading of GitHub: omitted checks and contexts are cleared.
type fakeChecks struct {
	strict   bool
	contexts []string
	checks   []map[string]any // context, app_id (nil: any source)
	noChecks bool             // the GET reports contexts only
}

// readOnly are the ruleset fields a PUT must not send.
var readOnly = []string{"id", "source", "source_type", "node_id", "_links", "created_at", "updated_at", "current_user_can_bypass"}

func clone(v any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func (f *fakeGitHub) serveRules(method string, u *url.URL, in map[string]any) (any, error) {
	base := f.base()
	p := u.Path
	switch {
	case method == http.MethodGet && p == base+"/rules/branches/"+f.defaultBranch:
		if page := u.Query().Get("page"); page != "1" || u.Query().Get("per_page") != "100" {
			return []any{}, nil
		}
		var ids []int64
		for id := range f.rulesets {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
		out := []any{}
		for _, id := range ids {
			rs := f.rulesets[id]
			if rs["enforcement"] != "active" {
				continue
			}
			rules, _ := rs["rules"].([]any)
			for _, r := range rules {
				r := clone(r)
				r["ruleset_id"], r["ruleset_source_type"], r["ruleset_source"] = id, rs["source_type"], rs["source"]
				out = append(out, r)
			}
		}
		return out, nil
	case strings.HasPrefix(p, base+"/rulesets/"):
		id, err := strconv.ParseInt(strings.TrimPrefix(p, base+"/rulesets/"), 10, 64)
		rs := f.rulesets[id]
		if err != nil || rs == nil {
			return nil, httpErr(404, "Not Found")
		}
		if method == http.MethodGet {
			out := clone(rs)
			out["id"], out["node_id"] = id, "RRS_x"
			out["_links"] = map[string]any{"html": map[string]string{"href": fmt.Sprintf("https://github.com/%v/settings/rules/%d", rs["source"], id)}}
			return out, nil
		}
		if method != http.MethodPut || rs["source_type"] != sourceRepo {
			return nil, fmt.Errorf("unexpected %s %s", method, p)
		}
		for _, k := range readOnly {
			if _, ok := in[k]; ok {
				return nil, fmt.Errorf("ruleset PUT sends the read-only %s", k)
			}
		}
		// The whole ruleset is replaced: an omitted field is lost.
		next := clone(in)
		next["source_type"], next["source"] = rs["source_type"], rs["source"]
		f.rulesets[id] = next
		f.rulesetPUTs++
		return map[string]any{}, nil
	case p == base+"/branches/"+f.defaultBranch+"/protection/required_status_checks":
		c := f.protection
		if c == nil {
			return nil, httpErr(404, "Branch not protected")
		}
		if method == http.MethodGet {
			out := map[string]any{"strict": c.strict, "contexts": c.contexts, "url": "x"}
			if !c.noChecks {
				out["checks"] = c.checks
			}
			return out, nil
		}
		if method != http.MethodPatch {
			return nil, fmt.Errorf("unexpected %s %s", method, p)
		}
		checks, hasChecks := in["checks"].([]any)
		contexts, hasContexts := in["contexts"].([]any)
		if hasChecks && hasContexts {
			return nil, fmt.Errorf("both checks and contexts")
		}
		c.strict = in["strict"].(bool)
		c.contexts, c.checks = []string{}, []map[string]any{}
		for _, x := range checks {
			m := x.(map[string]any)
			app := m["app_id"]
			if app == float64(-1) {
				app = nil
			}
			c.checks = append(c.checks, map[string]any{"context": m["context"], "app_id": app})
			c.contexts = append(c.contexts, m["context"].(string))
		}
		for _, x := range contexts {
			c.checks = append(c.checks, map[string]any{"context": x, "app_id": nil})
			c.contexts = append(c.contexts, x.(string))
		}
		return map[string]any{}, nil
	}
	return nil, fmt.Errorf("unexpected %s %s", method, p)
}

// repoRuleset is a repository ruleset that requires up to date, with other
// rules, conditions and bypass actors.
func repoRuleset(strict bool) map[string]any {
	return clone(map[string]any{
		"name": "main rules", "target": "branch", "enforcement": "active", "source_type": sourceRepo, "source": "o/app",
		"conditions":    map[string]any{"ref_name": map[string]any{"include": []string{"~DEFAULT_BRANCH"}, "exclude": []string{}}},
		"bypass_actors": []any{map[string]any{"actor_id": 5, "actor_type": "RepositoryRole", "bypass_mode": "always"}},
		"rules": []any{
			map[string]any{"type": "deletion"},
			map[string]any{"type": "pull_request", "parameters": map[string]any{"required_approving_review_count": 1, "allowed_merge_methods": []string{"rebase"}}},
			map[string]any{"type": ruleStatus, "parameters": map[string]any{
				strictParam: strict, "do_not_enforce_on_create": false,
				"required_status_checks": []any{map[string]any{"context": "CI", "integration_id": 15368}},
			}},
		},
	})
}

func strictOf(rs map[string]any) bool {
	for _, r := range rs["rules"].([]any) {
		m := r.(map[string]any)
		if m["type"] == ruleStatus {
			return m["parameters"].(map[string]any)[strictParam].(bool)
		}
	}
	return false
}

func stepByID(p op.Plan, id string) (op.Step, bool) { return findStep(p.Steps, id) }

func TestInitMergeMethods(t *testing.T) {
	f := newFake("app")
	stub(t, f, execRunners())
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := stepByID(p, settingItem("merge_method"))
	if s.NoChange || s.Current != "merge, squash, rebase" || s.Next != "rebase" || s.Target != "rebase only" {
		t.Errorf("merge item = %+v", s)
	}
	want := map[string]bool{"allow_merge_commit": false, "allow_squash_merge": false, "allow_rebase_merge": true}
	if !reflect.DeepEqual(f.merges, want) || !slices.Equal(r["mergeMethods"].([]string), []string{"rebase"}) {
		t.Errorf("merges %v, result %v", f.merges, r["mergeMethods"])
	}
	if c := itemChecks(t, r)[settingItem("merge_method")]; !c.Confirmed || c.State != "rebase" {
		t.Errorf("merge check = %+v", c)
	}

	// Already rebase only: no change, nothing written for it.
	p, _, err = run(t)
	if s, _ = stepByID(p, settingItem("merge_method")); err != nil || !s.NoChange || s.Detail != "already rebase only" {
		t.Errorf("rerun: %v, %+v", err, s)
	}

	// Unreported fields are written and, still unreported, not confirmed.
	f = newFake("app")
	f.dropMerges = true
	stub(t, f, execRunners())
	p, r, err = run(t)
	if s, _ = stepByID(p, settingItem("merge_method")); s.NoChange || s.Current != "not reported by GitHub" {
		t.Errorf("unreported item = %+v", s)
	}
	if op.Code(err) != op.CodeNotApplied || !reflect.DeepEqual(f.merges, want) {
		t.Errorf("unreported: %v, merges %v", err, f.merges)
	}
	if c := itemChecks(t, r)[settingItem("merge_method")]; c.Confirmed || c.State != "not reported by GitHub" {
		t.Errorf("unreported check = %+v", c)
	}
}

func TestInitUpToDateNone(t *testing.T) {
	for _, code := range []int{0, 403, 404} {
		f := newFake("app")
		if code != 0 {
			f.fail = map[string]error{"GET repos/o/app/rules/branches/main": httpErr(code, "Upgrade to GitHub Pro")}
		}
		stub(t, f, execRunners())
		p, r, err := run(t)
		if err != nil {
			t.Fatalf("%d: %v", code, err)
		}
		var items []op.Step
		for _, s := range p.Steps {
			if strings.HasPrefix(s.ID, upToDateItem) {
				items = append(items, s)
			}
		}
		if len(items) != 1 || items[0].ID != upToDateItem || !items[0].NoChange {
			t.Errorf("%d: items = %+v", code, items)
		}
		if c := itemChecks(t, r)[upToDateItem]; !c.Confirmed {
			t.Errorf("%d: check = %+v", code, c)
		}
		for _, w := range f.writes() {
			if strings.Contains(w, "rulesets") || strings.Contains(w, "protection") {
				t.Errorf("%d: write %s", code, w)
			}
		}
	}

	// The protection endpoint's feature-unavailable 403 also means none.
	f := newFake("app")
	f.fail = map[string]error{"GET repos/o/app/branches/main/protection/required_status_checks": httpErr(403, "Upgrade to GitHub Pro or make this repository public to enable this feature.")}
	stub(t, f, execRunners())
	if _, _, err := run(t); err != nil {
		t.Errorf("protection 403 unavailable: %v", err)
	}

	// Another failure, including any other 403, stops the plan.
	for _, path := range []string{
		"GET repos/o/app/rules/branches/main",
		"GET repos/o/app/branches/main/protection/required_status_checks",
	} {
		for _, e := range []error{
			httpErr(502, "boom"),
			httpErr(403, "You have exceeded a secondary rate limit."),
			httpErr(403, "Resource not accessible by integration"),
		} {
			f := newFake("app")
			f.rulesets[7] = repoRuleset(true)
			f.fail = map[string]error{path: e}
			stub(t, f, execRunners())
			if _, _, err := run(t); op.Code(err) != op.CodeGitHubAPIFailed || len(f.writes()) != 0 {
				t.Errorf("%s %v: %v, %q", path, e, err, f.writes())
			}
		}
	}
}

func TestInitUpToDateRuleset(t *testing.T) {
	f := newFake("app")
	f.rulesets[7] = repoRuleset(true)
	f.rulesets[8] = repoRuleset(true)
	f.rulesets[8]["enforcement"] = "disabled" // not applied to the branch
	stub(t, f, execRunners())
	o := New(op.RepoParams{Repo: "github.com/o/app"})
	ctx := context.Background()
	p, err := o.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := stepByID(p, rulesetItem(7))
	if !ok || s.NoChange || s.Current != "required" || s.Next != "off" || s.Target != "ruleset main rules" {
		t.Errorf("item = %+v", s)
	}
	if _, ok := stepByID(p, rulesetItem(8)); ok {
		t.Error("a disabled ruleset is planned")
	}
	// A bypass actor added after the plan is kept.
	f.rulesets[7]["bypass_actors"] = append(f.rulesets[7]["bypass_actors"].([]any), map[string]any{"actor_id": 9, "actor_type": "Team", "bypass_mode": "pull_request"})
	want := clone(f.rulesets[7])
	if err := o.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	r, err := o.ReadBack(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	got := f.rulesets[7]
	if strictOf(got) || f.rulesetPUTs != 1 {
		t.Errorf("strict %v, PUTs %d", strictOf(got), f.rulesetPUTs)
	}
	// Everything but the flag is as before.
	want["rules"].([]any)[2].(map[string]any)["parameters"].(map[string]any)[strictParam] = false
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ruleset = %v\nwant %v", got, want)
	}
	if !strictOf(f.rulesets[8]) {
		t.Error("the disabled ruleset changed")
	}
	if c := itemChecks(t, r)[rulesetItem(7)]; !c.Confirmed || c.State != "off" {
		t.Errorf("check = %+v", c)
	}
	if u := r["upToDate"].([]map[string]any); len(u) != 1 || u[0]["name"] != "main rules" || u[0]["strict"] != false {
		t.Errorf("upToDate = %v", u)
	}

	// Already off: no change, no write.
	p, _, err = run(t)
	if s, _ := stepByID(p, rulesetItem(7)); err != nil || !s.NoChange || f.rulesetPUTs != 1 {
		t.Errorf("rerun: %v, %+v, PUTs %d", err, s, f.rulesetPUTs)
	}
}

func TestInitUpToDateRulesetIncomplete(t *testing.T) {
	for _, drop := range []string{"bypass_actors", "checks"} {
		f := newFake("app")
		f.rulesets[7] = repoRuleset(true)
		if drop == "bypass_actors" {
			delete(f.rulesets[7], "bypass_actors")
		} else {
			delete(f.rulesets[7]["rules"].([]any)[2].(map[string]any)["parameters"].(map[string]any), "required_status_checks")
		}
		before := clone(f.rulesets[7])
		stub(t, f, execRunners())
		var failed []string
		ctx := op.WithProgress(context.Background(), func(e op.Event) {
			if e.Phase == op.PhaseFailed {
				failed = append(failed, e.ID+" "+e.Code)
			}
		})
		_, _, err := op.Run(ctx, New(op.RepoParams{Repo: "github.com/o/app"}), yes{})
		if op.Code(err) != op.CodeRepoUnreadable || !strings.Contains(err.Error(), "https://github.com/o/app/settings/rules/7") {
			t.Errorf("%s: %v", drop, err)
		}
		if f.rulesetPUTs != 0 || !reflect.DeepEqual(f.rulesets[7], before) || !slices.Equal(failed, []string{rulesetItem(7) + " " + op.CodeRepoUnreadable}) {
			t.Errorf("%s: PUTs %d, failed %q", drop, f.rulesetPUTs, failed)
		}
	}
}

func TestInitUpToDateInherited(t *testing.T) {
	f := newFake("app")
	org := repoRuleset(true)
	org["source_type"], org["source"], org["name"] = "Organization", "o", "org rules"
	f.rulesets[3] = org
	off := clone(org)
	off["name"] = "org off"
	off["rules"].([]any)[2].(map[string]any)["parameters"].(map[string]any)[strictParam] = false
	f.rulesets[4] = off
	stub(t, f, execRunners())
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	note := "ruleset org rules of the organization o requires branches to be up to date before merging; init does not change it: turn it off at https://github.com/o/settings/rules/3"
	if !slices.Contains(p.Notes, note) || len(p.Notes) != 2 {
		t.Errorf("notes = %q", p.Notes)
	}
	if notes := r[op.ResultNotes].([]string); !slices.Contains(notes, note) {
		t.Errorf("result notes = %q", notes)
	}
	if s, ok := stepByID(p, upToDateItem); !ok || !s.NoChange {
		t.Errorf("items = %q", allActions(p))
	}
	if u := r["upToDate"].([]map[string]any); len(u) != 1 || u[0]["source"] != "organization ruleset" || u[0]["strict"] != true {
		t.Errorf("upToDate = %v", u)
	}
	if f.rulesetPUTs != 0 || !strictOf(f.rulesets[3]) {
		t.Errorf("inherited written: %d", f.rulesetPUTs)
	}
}

func TestInitUpToDateBranchProtection(t *testing.T) {
	for _, noChecks := range []bool{false, true} {
		f := newFake("app")
		f.protection = &fakeChecks{strict: true, contexts: []string{"CI", "lint"}, noChecks: noChecks,
			checks: []map[string]any{{"context": "CI", "app_id": 15368}, {"context": "lint", "app_id": nil}}}
		if noChecks {
			f.protection.checks = []map[string]any{{"context": "CI", "app_id": nil}, {"context": "lint", "app_id": nil}}
		}
		wantChecks := clone(map[string]any{"c": f.protection.checks})["c"]
		stub(t, f, execRunners())
		p, r, err := run(t)
		if err != nil {
			t.Fatalf("noChecks %v: %v", noChecks, err)
		}
		if s, ok := stepByID(p, protectionItem); !ok || s.NoChange || s.Current != "required" || s.Target != "branch protection of main" {
			t.Errorf("item = %+v", s)
		}
		if _, ok := stepByID(p, upToDateItem); ok {
			t.Error("the none item next to a protection")
		}
		c := f.protection
		if c.strict || !slices.Equal(c.contexts, []string{"CI", "lint"}) || !reflect.DeepEqual(clone(map[string]any{"c": c.checks})["c"], wantChecks) {
			t.Errorf("noChecks %v: protection = %+v", noChecks, c)
		}
		if ch := itemChecks(t, r)[protectionItem]; !ch.Confirmed {
			t.Errorf("check = %+v", ch)
		}
		if !slices.Contains(f.writes(), "PATCH repos/o/app/branches/main/protection/required_status_checks") {
			t.Errorf("writes = %q", f.writes())
		}
	}

	// Already off: no change, no write.
	f := newFake("app")
	f.protection = &fakeChecks{contexts: []string{"CI"}, checks: []map[string]any{{"context": "CI", "app_id": nil}}}
	stub(t, f, execRunners())
	p, _, err := run(t)
	if s, _ := stepByID(p, protectionItem); err != nil || !s.NoChange || slices.ContainsFunc(f.writes(), func(w string) bool { return strings.Contains(w, "protection") }) {
		t.Errorf("off: %v, %+v, %q", err, s, f.writes())
	}
}

func TestInitUpToDateReadBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(f *fakeGitHub)
		item   string
		reason string
	}{
		{"ruleset strict again", func(f *fakeGitHub) {
			f.rulesets[7]["rules"].([]any)[2].(map[string]any)["parameters"].(map[string]any)[strictParam] = true
		}, rulesetItem(7), "still requires"},
		{"ruleset enforcement", func(f *fakeGitHub) { f.rulesets[7]["enforcement"] = "evaluate" }, rulesetItem(7), "changed beyond up to date"},
		{"ruleset bypass", func(f *fakeGitHub) { f.rulesets[7]["bypass_actors"] = []any{} }, rulesetItem(7), "changed beyond up to date"},
		{"protection strict again", func(f *fakeGitHub) { f.protection.strict = true }, protectionItem, "still requires"},
		{"protection checks", func(f *fakeGitHub) { f.protection.checks = nil; f.protection.contexts = nil }, protectionItem, "changed beyond up to date"},
	} {
		f := newFake("app")
		f.rulesets[7] = repoRuleset(true)
		f.protection = &fakeChecks{strict: true, contexts: []string{"CI"}, checks: []map[string]any{{"context": "CI", "app_id": 15368}}}
		stub(t, f, execRunners())
		o := New(op.RepoParams{Repo: "github.com/o/app"})
		ctx := context.Background()
		p, err := o.Plan(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := o.Apply(ctx, p); err != nil {
			t.Fatal(err)
		}
		tc.change(f)
		r, err := o.ReadBack(ctx, p)
		if op.Code(err) != op.CodeNotApplied || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: %v", tc.name, err)
		}
		checks := itemChecks(t, r)
		if c := checks[tc.item]; c.Confirmed || !strings.Contains(c.Reason, tc.reason) {
			t.Errorf("%s: check = %+v", tc.name, c)
		}
		other := protectionItem
		if tc.item == protectionItem {
			other = rulesetItem(7)
		}
		if c := checks[other]; !c.Confirmed {
			t.Errorf("%s: other = %+v", tc.name, c)
		}
	}

	// The none item fails read-back when a protection appears meanwhile.
	f := newFake("app")
	stub(t, f, execRunners())
	o := New(op.RepoParams{Repo: "github.com/o/app"})
	ctx := context.Background()
	p, err := o.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	f.protection = &fakeChecks{strict: true, contexts: []string{}, checks: []map[string]any{}}
	r, err := o.ReadBack(ctx, p)
	if c := itemChecks(t, r)[upToDateItem]; op.Code(err) != op.CodeNotApplied || c.Confirmed {
		t.Errorf("none: %v, %+v", err, c)
	}
}
