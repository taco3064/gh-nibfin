package upgrade

import (
	"context"
	"reflect"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// TestUpgradeReportsRepositoryItems checks that the repository items
// upgrade reports while it plans are its plan's items, with the same values
// and in plan order. The host part reports its own items (see op).
func TestUpgradeReportsRepositoryItems(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"current", render(t, templates.Peer, imageRef)},
		{"stale", stale(t, templates.Peer)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub(t, newFake(tc.files))
			useHostPart(t, &fakeHostPart{steps: hostSteps})
			var checks []string
			var checked []op.Step
			ctx := op.WithPlanProgress(context.Background(), func(e op.PlanEvent) {
				if e.Kind == op.PlanChecking {
					checks = append(checks, e.Check)
				} else {
					checked = append(checked, e.Steps...)
				}
			})
			p, err := New(op.RepoParams{Repo: repo}).Plan(ctx)
			if err != nil {
				t.Fatal(err)
			}
			repoSteps := p.Steps[:len(p.Steps)-len(hostSteps)]
			if len(repoSteps) == 0 || !reflect.DeepEqual(checked, repoSteps) {
				t.Errorf("checked items differ from the plan:\nchecked %+v\nplan    %+v", checked, repoSteps)
			}
			if len(checks) < 3 {
				t.Errorf("checks %q", checks)
			}
		})
	}
}
