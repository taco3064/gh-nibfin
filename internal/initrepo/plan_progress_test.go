package initrepo

import (
	"context"
	"reflect"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// TestInitReportsItsItems checks that the items init reports while it plans
// are its plan's items, with the same values and in plan order.
func TestInitReportsItsItems(t *testing.T) {
	for _, tc := range []struct {
		name    string
		repo    string
		runners *fakeRunners
	}{
		{"execution", "app", execRunners()},
		{"peer", "lib", execRunners()},
		{"stopped", "app", &fakeRunners{stopped: true, repo: "github.com/o/app", providers: []string{"codex"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub(t, newFake(tc.repo), tc.runners)
			var checks []string
			var checked []op.Step
			ctx := op.WithPlanProgress(context.Background(), func(e op.PlanEvent) {
				if e.Kind == op.PlanChecking {
					checks = append(checks, e.Check)
				} else {
					checked = append(checked, e.Steps...)
				}
			})
			p, err := New(op.RepoParams{Repo: "github.com/o/" + tc.repo}).Plan(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(checked, p.Steps) {
				t.Errorf("checked items differ from the plan:\nchecked %+v\nplan    %+v", checked, p.Steps)
			}
			if len(checks) == 0 || checks[0] != "repository github.com/o/"+tc.repo {
				t.Errorf("checks %q", checks)
			}
		})
	}
}
