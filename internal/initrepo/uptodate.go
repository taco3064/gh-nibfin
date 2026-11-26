package initrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/workflowpr"
)

// The up-to-date requirement is GitHub's "Require branches to be up to date
// before merging": strict_required_status_checks_policy in a ruleset's
// required_status_checks rule, strict in branch protection. Init turns it off
// in every repository ruleset and branch protection on the default branch and
// changes nothing else. Inherited rulesets are reported in a note.

const (
	upToDateItem   = "up-to-date"
	protectionItem = "up-to-date:branch-protection"
	upToDateAction = "turn off require up to date"
	upToDateDetail = "Require branches to be up to date before merging"
	ruleStatus     = "required_status_checks"
	strictParam    = "strict_required_status_checks_policy"
	sourceRepo     = "Repository"
)

func rulesetItem(id int64) string { return fmt.Sprintf("up-to-date:ruleset:%d", id) }

// ruleset is a ruleset as GitHub returns it, kept field by field so a write
// sends back exactly what was read.
type ruleset map[string]json.RawMessage

// statusChecks is the required_status_checks of branch protection. A nil
// slice is a field GitHub did not report.
type statusChecks struct {
	Strict   bool          `json:"strict"`
	Contexts []string      `json:"contexts"`
	Checks   []statusCheck `json:"checks"`
}

type statusCheck struct {
	Context string `json:"context"`
	AppID   *int64 `json:"app_id"`
}

// upToDateSource is one repository ruleset or the branch protection of the
// default branch. The reference copy is the one read at plan time, then the
// fresh copy read right before the write.
type upToDateSource struct {
	id         int64 // the ruleset; 0 for branch protection
	name       string
	strict     bool
	url        string
	ruleset    ruleset
	protection *statusChecks
}

type inheritedRuleset struct {
	id                       int64
	name, sourceType, source string
}

type upToDatePlan struct {
	sources   []upToDateSource
	inherited []inheritedRuleset // those that require up to date
	notes     []string
}

func (s upToDateSource) step() op.Step {
	st := op.Step{ID: rulesetItem(s.id), Action: upToDateAction, Target: "ruleset " + s.name, Detail: upToDateDetail, Current: "off", Next: "off", NoChange: !s.strict}
	if s.protection != nil {
		st.ID, st.Target = protectionItem, s.name
	}
	if s.strict {
		st.Current = "required"
	} else {
		st.Detail = "already off"
	}
	return st
}

func noneStep() op.Step {
	return op.Step{ID: upToDateItem, Action: upToDateAction, Target: "default branch", Detail: "no ruleset or branch protection of the repository requires status checks",
		Current: "not required", Next: "not required", NoChange: true}
}

func (u upToDatePlan) steps() []op.Step {
	if len(u.sources) == 0 {
		return []op.Step{noneStep()}
	}
	var out []op.Step
	for _, s := range u.sources {
		out = append(out, s.step())
	}
	return out
}

// branchRule is one active rule GitHub applies to a branch.
type branchRule struct {
	Type       string `json:"type"`
	Parameters struct {
		Strict bool `json:"strict_required_status_checks_policy"`
	} `json:"parameters"`
	RulesetID  int64  `json:"ruleset_id"`
	SourceType string `json:"ruleset_source_type"`
	Source     string `json:"ruleset_source"`
}

// unavailable is a GitHub answer that means the feature does not apply: no
// such rules or protection, or a plan without them.
func unavailable(err error) bool {
	return workflowpr.IsStatus(err, http.StatusNotFound) || workflowpr.IsStatus(err, http.StatusForbidden)
}

func (i *initRepo) branchPath() string {
	return i.repo.Base + "/branches/" + workflowpr.EscapeRef(i.info.DefaultBranch)
}

// branchRules reads the required_status_checks rules on the default branch.
func (i *initRepo) branchRules(ctx context.Context) ([]branchRule, error) {
	const perPage = 100
	var out []branchRule
	for page := 1; ; page++ {
		var got []branchRule
		p := fmt.Sprintf("%s/rules/branches/%s?per_page=%d&page=%d", i.repo.Base, workflowpr.EscapeRef(i.info.DefaultBranch), perPage, page)
		err := i.get(ctx, p, &got)
		if unavailable(err) {
			return nil, nil
		}
		if err != nil {
			return nil, apiErr("read the rules of the branch "+i.info.DefaultBranch+" of "+i.p.Repo, err)
		}
		for _, r := range got {
			if r.Type == ruleStatus {
				out = append(out, r)
			}
		}
		if len(got) < perPage {
			return out, nil
		}
	}
}

func (i *initRepo) rulesetPath(id int64) string {
	return fmt.Sprintf("%s/rulesets/%d", i.repo.Base, id)
}

func (i *initRepo) readRuleset(ctx context.Context, id int64) (ruleset, error) {
	var rs ruleset
	if err := i.get(ctx, i.rulesetPath(id), &rs); err != nil {
		return nil, apiErr(fmt.Sprintf("read the ruleset %d of %s", id, i.p.Repo), err)
	}
	return rs, nil
}

// readStatusChecks reads the branch protection's required status checks;
// found is false when the branch has none.
func (i *initRepo) readStatusChecks(ctx context.Context) (sc statusChecks, found bool, err error) {
	err = i.get(ctx, i.branchPath()+"/protection/required_status_checks", &sc)
	if unavailable(err) {
		return statusChecks{}, false, nil
	}
	if err != nil {
		return sc, false, apiErr("read the required status checks of the branch "+i.info.DefaultBranch+" of "+i.p.Repo, err)
	}
	return sc, true, nil
}

func (rs ruleset) str(key string) string {
	var s string
	_ = json.Unmarshal(rs[key], &s)
	return s
}

// strict reports whether a required_status_checks rule of rs requires up to
// date.
func (rs ruleset) strict() bool {
	var rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			Strict bool `json:"strict_required_status_checks_policy"`
		} `json:"parameters"`
	}
	_ = json.Unmarshal(rs["rules"], &rules)
	for _, r := range rules {
		if r.Type == ruleStatus && r.Parameters.Strict {
			return true
		}
	}
	return false
}

// htmlURL is the ruleset's settings page, or fallback.
func (rs ruleset) htmlURL(fallback string) string {
	var links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	}
	if json.Unmarshal(rs["_links"], &links) == nil && links.HTML.Href != "" {
		return links.HTML.Href
	}
	return fallback
}

func present(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// rulesetBody is the PUT body that writes rs back with up to date off: every
// writable field as read, read-only fields left out. A ruleset GitHub did not
// return in full is not written, since the PUT could reset what is missing.
func rulesetBody(rs ruleset) (map[string]json.RawMessage, error) {
	body := map[string]json.RawMessage{}
	for _, k := range []string{"name", "target", "enforcement", "bypass_actors", "rules"} {
		if !present(rs[k]) {
			return nil, fmt.Errorf("GitHub did not return its %s", k)
		}
		body[k] = rs[k]
	}
	if present(rs["conditions"]) {
		body["conditions"] = rs["conditions"]
	}
	var rules []map[string]json.RawMessage
	if err := json.Unmarshal(rs["rules"], &rules); err != nil {
		return nil, fmt.Errorf("its rules: %w", err)
	}
	for _, r := range rules {
		var typ string
		_ = json.Unmarshal(r["type"], &typ)
		if typ != ruleStatus {
			continue
		}
		var params map[string]json.RawMessage
		if err := json.Unmarshal(r["parameters"], &params); err != nil || !present(params["required_status_checks"]) {
			return nil, fmt.Errorf("GitHub did not return the checks of its %s rule", ruleStatus)
		}
		params[strictParam] = json.RawMessage("false")
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		r["parameters"] = b
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	body["rules"] = b
	return body, nil
}

// planUpToDate reads the rulesets and the branch protection on the default
// branch. Only repository rulesets are written; inherited ones that require
// up to date become notes.
func (i *initRepo) planUpToDate(ctx context.Context) (upToDatePlan, error) {
	var u upToDatePlan
	rules, err := i.branchRules(ctx)
	if err != nil {
		return u, err
	}
	var ids []int64
	byID := map[int64]branchRule{}
	for _, r := range rules {
		if r.RulesetID == 0 {
			return u, op.Errorf(op.CodeRepoUnreadable, "read the rules of the branch %s of %s: GitHub did not name the ruleset of a %s rule", i.info.DefaultBranch, i.p.Repo, ruleStatus)
		}
		g, seen := byID[r.RulesetID]
		if !seen {
			ids = append(ids, r.RulesetID)
			g = r
		}
		g.Parameters.Strict = g.Parameters.Strict || r.Parameters.Strict
		byID[r.RulesetID] = g
	}
	slices.Sort(ids)
	for _, id := range ids {
		g := byID[id]
		if g.SourceType == sourceRepo {
			rs, err := i.readRuleset(ctx, id)
			if err != nil {
				return u, err
			}
			u.sources = append(u.sources, upToDateSource{id: id, name: rs.str("name"), strict: rs.strict(), url: rs.htmlURL(i.rulesURL(id)), ruleset: rs})
			continue
		}
		if !g.Parameters.Strict {
			continue
		}
		// An inherited ruleset is changed where it is defined; its name and
		// link are best effort.
		in := inheritedRuleset{id: id, name: fmt.Sprint(id), sourceType: g.SourceType, source: g.Source}
		link := i.inheritedURL(g)
		if rs, err := i.readRuleset(ctx, id); err == nil {
			if n := rs.str("name"); n != "" {
				in.name = n
			}
			link = rs.htmlURL(link)
		}
		u.inherited = append(u.inherited, in)
		u.notes = append(u.notes, fmt.Sprintf("ruleset %s of the %s %s requires branches to be up to date before merging; init does not change it: turn it off at %s",
			in.name, strings.ToLower(g.SourceType), g.Source, link))
	}
	sc, found, err := i.readStatusChecks(ctx)
	if err != nil {
		return u, err
	}
	if found {
		u.sources = append(u.sources, upToDateSource{name: "branch protection of " + i.info.DefaultBranch, strict: sc.Strict, url: i.settingsURL() + "/branches", protection: &sc})
	}
	return u, nil
}

func (i *initRepo) rulesURL(id int64) string {
	return fmt.Sprintf("%s/rules/%d", i.settingsURL(), id)
}

func (i *initRepo) inheritedURL(r branchRule) string {
	if r.SourceType == "Organization" && r.Source != "" {
		return fmt.Sprintf("https://%s/organizations/%s/settings/rules/%d", i.host, r.Source, r.RulesetID)
	}
	return i.settingsURL() + "/rules"
}

// applyUpToDate turns up to date off in each source that requires it.
func (i *initRepo) applyUpToDate(ctx context.Context) error {
	for k := range i.upToDate.sources {
		s := &i.upToDate.sources[k]
		if !s.strict {
			continue
		}
		write := i.writeRuleset
		if s.protection != nil {
			write = i.writeProtection
		}
		if err := op.Track(ctx, s.step(), func() error { return write(ctx, s) }); err != nil {
			return err
		}
	}
	return nil
}

// writeRuleset reads the ruleset again and writes that copy back with up to
// date off, so a change made since the plan is kept.
func (i *initRepo) writeRuleset(ctx context.Context, s *upToDateSource) error {
	fresh, err := i.readRuleset(ctx, s.id)
	if err != nil {
		return err
	}
	body, err := rulesetBody(fresh)
	if err != nil {
		return op.Errorf(op.CodeRepoUnreadable, "turn off up to date in the ruleset %s of %s: %v; init does not write it, since that could reset what is missing; change it at %s", s.name, i.p.Repo, err, s.url)
	}
	s.ruleset = fresh
	if err := i.send(ctx, http.MethodPut, i.rulesetPath(s.id), body, nil); err != nil {
		return apiErr(fmt.Sprintf("turn off up to date in the ruleset %s of %s (change it at %s)", s.name, i.p.Repo, s.url), err)
	}
	return nil
}

// writeProtection reads the required status checks again and writes strict
// off with the same checks.
func (i *initRepo) writeProtection(ctx context.Context, s *upToDateSource) error {
	fresh, found, err := i.readStatusChecks(ctx)
	if err != nil {
		return err
	}
	if !found {
		return op.Errorf(op.CodeNotFound, "the %s of %s no longer requires status checks; plan again", s.name, i.p.Repo)
	}
	body := map[string]any{"strict": false}
	switch {
	case fresh.Checks != nil:
		checks := []map[string]any{}
		for _, c := range fresh.Checks {
			// No app is "any source", which GitHub writes as -1.
			app := int64(-1)
			if c.AppID != nil {
				app = *c.AppID
			}
			checks = append(checks, map[string]any{"context": c.Context, "app_id": app})
		}
		body["checks"] = checks
	case fresh.Contexts != nil:
		body["contexts"] = fresh.Contexts
	default:
		return op.Errorf(op.CodeRepoUnreadable, "turn off up to date in the %s of %s: GitHub did not return its required checks; init does not write it, since that could clear them; change it at %s", s.name, i.p.Repo, s.url)
	}
	s.protection = &fresh
	if err := i.send(ctx, http.MethodPatch, i.branchPath()+"/protection/required_status_checks", body, nil); err != nil {
		return apiErr(fmt.Sprintf("turn off up to date in the %s of %s (change it at %s)", s.name, i.p.Repo, s.url), err)
	}
	return nil
}

// decoded is raw as a comparable value; absent and null are equal.
func decoded(raw json.RawMessage) any {
	var v any
	if present(raw) {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}

// rulesWithoutStrict are the rules with up to date set to off, to compare
// everything else.
func rulesWithoutStrict(raw json.RawMessage) any {
	rules, _ := decoded(raw).([]any)
	for _, r := range rules {
		m, _ := r.(map[string]any)
		if m == nil || m["type"] != ruleStatus {
			continue
		}
		if params, _ := m["parameters"].(map[string]any); params != nil {
			params[strictParam] = false
		}
	}
	return rules
}

// sameRuleset reports whether b equals a apart from the up-to-date flag.
func sameRuleset(a, b ruleset) bool {
	for _, k := range []string{"name", "target", "enforcement", "conditions", "bypass_actors"} {
		if !reflect.DeepEqual(decoded(a[k]), decoded(b[k])) {
			return false
		}
	}
	return reflect.DeepEqual(rulesWithoutStrict(a["rules"]), rulesWithoutStrict(b["rules"]))
}

func appID(c statusCheck) int64 {
	if c.AppID == nil {
		return -1
	}
	return *c.AppID
}

func sameChecks(a, b statusChecks) bool {
	return slices.Equal(a.Contexts, b.Contexts) && slices.EqualFunc(a.Checks, b.Checks, func(x, y statusCheck) bool {
		return x.Context == y.Context && appID(x) == appID(y)
	})
}

// readBackUpToDate confirms each up-to-date item and that nothing else in
// its source changed.
func (i *initRepo) readBackUpToDate(ctx context.Context, checks *op.Checks, r op.Result, notApplied *[]string) error {
	u := i.upToDate
	out := []map[string]any{}
	defer func() { r["upToDate"] = out }()
	if len(u.sources) == 0 {
		now, err := i.planUpToDate(ctx)
		if err != nil {
			return err
		}
		var required []string
		for _, s := range now.sources {
			if s.strict {
				required = append(required, s.name)
			}
		}
		if len(required) > 0 {
			state := "required by " + strings.Join(required, ", ")
			*notApplied = append(*notApplied, "up to date is "+state)
			checks.Set(noneStep(), false, state, "up to date is "+state)
		} else {
			checks.Set(noneStep(), true, "not required", "")
		}
	}
	for _, s := range u.sources {
		step := s.step()
		var strict, same bool
		source := "repository ruleset"
		if s.protection != nil {
			source = "branch protection"
			got, found, err := i.readStatusChecks(ctx)
			if err != nil {
				return err
			}
			if !found {
				*notApplied = append(*notApplied, s.name+" is gone")
				checks.Set(step, false, "absent", s.name+" no longer requires status checks")
				out = append(out, map[string]any{"source": source, "name": s.name, "strict": false})
				continue
			}
			strict, same = got.Strict, sameChecks(*s.protection, got)
		} else {
			got, err := i.readRuleset(ctx, s.id)
			if err != nil {
				return err
			}
			strict, same = got.strict(), sameRuleset(s.ruleset, got)
		}
		out = append(out, map[string]any{"source": source, "name": s.name, "strict": strict})
		state := "off"
		if strict {
			state = "required"
		}
		switch {
		case strict:
			*notApplied = append(*notApplied, s.name+" still requires up to date")
			checks.Set(step, false, state, s.name+" still requires branches to be up to date")
		case !same:
			what := "ruleset " + s.name
			if s.protection != nil {
				what = s.name
			}
			*notApplied = append(*notApplied, what+" changed beyond up to date")
			checks.Set(step, false, state, what+" changed beyond up to date")
		default:
			checks.Set(step, true, state, "")
		}
	}
	for _, in := range u.inherited {
		out = append(out, map[string]any{"source": strings.ToLower(in.sourceType) + " ruleset", "name": in.name, "strict": true})
	}
	return nil
}
