package initrepo

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
	"github.com/taco3064/gh-nibfin/internal/workflowpr"
)

func itemChecks(t *testing.T, r op.Result) map[string]op.Check {
	t.Helper()
	items, ok := r[op.ResultItems].([]op.Check)
	if !ok {
		t.Fatalf("result without items: %v", r)
	}
	if _, ok := r[op.ResultNotes].([]string); !ok {
		t.Fatalf("result without notes: %v", r)
	}
	out := map[string]op.Check{}
	for _, c := range items {
		out[c.ID] = c
	}
	return out
}

func TestInitItemsAndProgress(t *testing.T) {
	f := newFake("app")
	stub(t, f, execRunners())
	var events []string
	ctx := op.WithProgress(context.Background(), func(e op.Event) { events = append(events, e.Phase+" "+e.ID) })
	p, r, err := op.Run(ctx, New(op.RepoParams{Repo: "github.com/o/app"}), yes{})
	if err != nil {
		t.Fatal(err)
	}
	// Every changing item starts and ends once; nothing fails.
	for _, s := range p.Changes() {
		if !slices.Contains(events, "started "+s.ID) || !slices.Contains(events, "done "+s.ID) {
			t.Errorf("item %s has no progress: %q", s.ID, events)
		}
	}
	for _, e := range events {
		if strings.HasPrefix(e, "failed") {
			t.Errorf("failed: %s", e)
		}
	}
	// The two settings are one PATCH: both start before either is done.
	pr, del := "started setting:pull_request_creation_policy", "started setting:delete_branch_on_merge"
	if slices.Index(events, del) > slices.Index(events, "done setting:pull_request_creation_policy") || slices.Index(events, pr) < 0 {
		t.Errorf("settings events = %q", events)
	}
	checks := itemChecks(t, r)
	if len(checks) != len(p.Steps) {
		t.Errorf("checks %v for steps %+v", checks, p.Steps)
	}
	for id, c := range checks {
		if !c.Confirmed {
			t.Errorf("%s not confirmed: %+v", id, c)
		}
	}
	if s := checks[secretItem]; s.State != "set" {
		t.Errorf("secret = %+v", s)
	}
	for _, s := range p.Steps {
		if s.ID == secretItem && (s.Current != "absent" || s.Next != "set") {
			t.Errorf("secret item = %+v", s)
		}
	}
}

func TestInitAlreadyInPlaceItems(t *testing.T) {
	f := newFake("app")
	f.commits["c0"][issueP] = templates.GeneratedPrefix + "\n"
	stub(t, f, execRunners())
	if _, _, err := run(t); err != nil {
		t.Fatal(err)
	}
	// A rerun: only the secret changes; every other item is in place.
	p, r, err := run(t)
	if err != nil {
		t.Fatal(err)
	}
	var changing []string
	for _, s := range p.Steps {
		if !s.NoChange {
			changing = append(changing, s.ID)
		}
	}
	if !slices.Equal(changing, []string{secretItem}) {
		t.Errorf("changing = %q", changing)
	}
	checks := itemChecks(t, r)
	for _, id := range []string{labelItem("shaped"), workflowpr.FileItem(issueP), workflowpr.PRItem, varItem(varExecRepo)} {
		if c, ok := checks[id]; !ok || !c.Confirmed {
			t.Errorf("%s = %+v", id, c)
		}
	}
}

func TestInitReadBackPartlyFails(t *testing.T) {
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
	f.fail = map[string]error{"GET repos/o/app/actions/secrets/" + secretName: httpErr(502, "boom")}
	r, err := o.ReadBack(ctx, p)
	if err == nil || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	checks := itemChecks(t, r)
	if c := checks[labelItem("shaped")]; !c.Confirmed {
		t.Errorf("label read before the failure = %+v", c)
	}
	if c := checks[secretItem]; c.Confirmed || !strings.Contains(c.Reason, "not read back") {
		t.Errorf("secret = %+v", c)
	}
	if c := checks[workflowpr.PRItem]; c.Confirmed {
		t.Errorf("PR read after the failure = %+v", c)
	}

	// A difference is not_applied, with the items.
	f.deleteOnMerge = true
	r, err = o.ReadBack(ctx, p)
	if op.Code(err) != op.CodeNotApplied || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	if c := itemChecks(t, r)[settingItem("delete_branch_on_merge")]; c.Confirmed || c.State != "true" {
		t.Errorf("setting = %+v", c)
	}
}

func TestInitFailedItem(t *testing.T) {
	f := newFake("app")
	f.fail = map[string]error{"POST repos/o/app/pulls": httpErr(502, "boom")}
	f.persist = true
	stub(t, f, execRunners())
	var failed, done []string
	ctx := op.WithProgress(context.Background(), func(e op.Event) {
		switch e.Phase {
		case op.PhaseFailed:
			failed = append(failed, e.ID+" "+e.Code)
		case op.PhaseDone:
			done = append(done, e.ID)
		}
	})
	_, _, err := op.Run(ctx, New(op.RepoParams{Repo: "github.com/o/app"}), yes{})
	if op.Code(err) != op.CodeRepoWriteFailed || !slices.Equal(failed, []string{"pr " + op.CodeRepoWriteFailed}) {
		t.Errorf("err %v, failed %q", err, failed)
	}
	if !slices.Contains(done, workflowpr.BranchItem(Branch)) || slices.Contains(done, workflowpr.PRItem) {
		t.Errorf("done = %q", done)
	}
}
