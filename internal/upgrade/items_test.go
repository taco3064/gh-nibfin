package upgrade

import (
	"context"
	"reflect"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
	"github.com/taco3064/gh-nibfin/internal/workflowpr"
)

func TestUpgradeItemsAndProgress(t *testing.T) {
	// The shared runner file is current; the issue and job files differ.
	f := newFake(stale(t, templates.Execution))
	stub(t, f)
	var events []string
	ctx := op.WithProgress(context.Background(), func(e op.Event) { events = append(events, e.Phase+" "+e.ID) })
	o := New(op.RepoParams{Repo: repo})
	p, err := o.Plan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file, job, shared := workflowpr.FileItem(issueP), workflowpr.FileItem(jobP), workflowpr.FileItem(sharedP)
	branch := workflowpr.BranchItem(Branch)
	var changing []string
	for _, s := range p.Steps {
		if s.ID == "" {
			t.Errorf("item without id: %+v", s)
		}
		if !s.NoChange {
			changing = append(changing, s.ID)
		}
		switch s.ID {
		case file:
			if s.Current != "differs" || s.Next != "current" {
				t.Errorf("changed file item = %+v", s)
			}
		case shared:
			if !s.NoChange || s.Current != "current" {
				t.Errorf("current file item = %+v", s)
			}
		}
	}
	if !reflect.DeepEqual(changing, []string{file, job, branch, workflowpr.PRItem}) || len(p.Steps) != len(changing)+2 {
		t.Fatalf("changing %q of %+v", changing, p.Steps)
	}
	if err := o.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	// The files and the branch are one commit; the PR is its own write.
	want := []string{"started " + file, "started " + job, "started " + branch, "done " + file, "done " + job, "done " + branch, "started pr", "done pr"}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %q", events)
	}
	r, err := o.ReadBack(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	items := checks(t, r)
	if len(items) != len(p.Steps) {
		t.Errorf("items %+v for steps %+v", items, p.Steps)
	}
	for _, c := range items {
		if !c.Confirmed {
			t.Errorf("not confirmed: %+v", c)
		}
	}

	// Someone pushed to the branch: every item is reported, not confirmed,
	// next to the error.
	f.commits["x1"] = f.commits[f.refs[Branch]]
	f.refs[Branch] = "x1"
	r, err = o.ReadBack(ctx, p)
	if op.Code(err) != op.CodeRepoWriteFailed || r == nil {
		t.Fatalf("r %v, err %v", r, err)
	}
	for _, c := range checks(t, r) {
		if c.Confirmed || c.Reason == "" {
			t.Errorf("confirmed after a failed read-back: %+v", c)
		}
	}
}

func TestUpgradeFailedItem(t *testing.T) {
	f := newFake(stale(t, templates.Peer))
	f.fail = map[string]error{"POST repos/o/r/pulls": notFound()}
	f.persist = true
	stub(t, f)
	var failed []string
	ctx := op.WithProgress(context.Background(), func(e op.Event) {
		if e.Phase == op.PhaseFailed {
			failed = append(failed, e.ID+" "+e.Code)
		}
	})
	_, _, err := op.Run(ctx, New(op.RepoParams{Repo: repo}), &confirm{})
	if err == nil || !reflect.DeepEqual(failed, []string{"pr " + op.Code(err)}) {
		t.Errorf("err %v, failed %q", err, failed)
	}
}
