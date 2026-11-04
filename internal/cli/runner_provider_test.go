package cli

import "testing"

type runnerListAPI struct{ value any }

func (f runnerListAPI) Call(_, _ string, _, output any) error {
	return assign(output, f.value)
}

func TestMissingOrNullListingCannotProveRunnerAbsence(t *testing.T) {
	for _, value := range []any{nil, map[string]any{},
		map[string]any{"total_count": 0},
		map[string]any{"total_count": 0, "runners": nil}} {
		if runnerAbsent(runnerListAPI{value: value}, "example/project", 42) == nil {
			t.Fatal("an incomplete native list cannot confirm deletion")
		}
	}
}
