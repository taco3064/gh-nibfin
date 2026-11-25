package status

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/op"
)

type partialCall struct {
	res     map[string]json.RawMessage
	loading []string
}

// recordPartials runs status with a sink and returns its calls.
func recordPartials(t *testing.T, repo string, each func(partialCall)) (op.Result, []partialCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []partialCall
	ctx := WithPartial(context.Background(), func(res op.Result, loading []string) {
		c := partialCall{res: map[string]json.RawMessage{}, loading: loading}
		for k, v := range res {
			raw, ok := v.(json.RawMessage)
			if !ok {
				t.Errorf("partial %s is %T, not a copy", k, v)
			}
			c.res[k] = raw
		}
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		if each != nil {
			each(c)
		}
	})
	_, res, err := op.ReadBack(ctx, New(op.RepoParams{Repo: repo}))
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return res, calls
}

// blockHost makes the host and latest-image reads wait for release.
func blockHost(release <-chan struct{}) {
	h, l := readHost, latestImage
	readHost = func(ctx context.Context) op.HostReport { <-release; return h(ctx) }
	latestImage = func(ctx context.Context) (string, error) { <-release; return l(ctx) }
}

func TestPartialRepoView(t *testing.T) {
	setup(t, repoRoutes())
	without := run(t, "github.com/o/r")

	fx := setup(t, repoRoutes())
	readHost = runners("github.com/o/r")
	release := make(chan struct{})
	blockHost(release)
	var once sync.Once
	res, calls := recordPartials(t, "github.com/o/r", func(c partialCall) {
		// The GitHub reads go on while the host is read; only the runs
		// wait for it.
		if slices.Contains(c.loading, "runs") && !slices.Contains(c.loading, "nibfinRepos") {
			once.Do(func() { close(release) })
		}
	})
	if len(calls) == 0 {
		t.Fatal("no partial")
	}
	first := calls[0]
	if _, ok := first.res["issues"]; !ok || !slices.Contains(first.loading, "host") || !slices.Contains(first.loading, "latestImage") {
		t.Fatalf("first partial = %v %v", first.loading, keys(first.res))
	}
	if want := []string{"host", "latestImage", "pendingComment", "blockedBy", "permission", "image", "nibfinRun", "nibfinRepos", "runs"}; !reflect.DeepEqual(first.loading, want) {
		t.Errorf("loading = %v", first.loading)
	}
	for _, k := range []string{"host", "latestImage", "image", "nibfinRun", "permission", "nibfinRepos"} {
		if _, ok := first.res[k]; ok {
			t.Errorf("first partial has %s", k)
		}
	}
	for i, c := range calls {
		if len(c.loading) == 0 {
			t.Errorf("partial %d loads nothing", i)
		}
		if i > 0 && len(c.loading) >= len(calls[i-1].loading) {
			t.Errorf("partial %d: loading %v after %v", i, c.loading, calls[i-1].loading)
		}
	}
	// A partial with the image but no latest image yet leaves latest null.
	for _, c := range calls {
		if img, ok := c.res["image"]; ok && slices.Contains(c.loading, "latestImage") && !strings.Contains(string(img), `"latest":null`) {
			t.Errorf("image before latest = %s", img)
		}
	}
	// The same final Result as without a sink, except the runner report.
	for _, k := range []string{"repo", "image", "issues", "nibfinRun", "permission", "nibfinRepos", "latestImage"} {
		if !sameJSON(t, res[k], without[k]) {
			t.Errorf("%s = %#v, without a sink %#v", k, res[k], without[k])
		}
	}
	if fx.graph.blockerQueries() == 0 {
		t.Error("no blocker query")
	}
}

func TestPartialPersonView(t *testing.T) {
	routes := personRoutes(item("a/one", 1, "scope"), item("b/two", 2, "scope", "pending"))
	routes["repos/a/one/contents/.github/workflows/nibfin-job.yml"] = file(jobYML(imgA))
	routes["repos/b/two/issues/2/events?per_page=100&page=1"] = []any{}
	setup(t, routes)
	readHost = runners("github.com/a/one")
	without := run(t, "")

	fx := setup(t, routes)
	fx.graph.viewer = []map[string]any{nibfinNode("a/one", "WRITE", true)}
	readHost = runners("github.com/a/one")
	release := make(chan struct{})
	blockHost(release)
	var once sync.Once
	res, calls := recordPartials(t, "", func(c partialCall) {
		if slices.Contains(c.loading, "runs") && !slices.Contains(c.loading, "nibfinRepos") {
			once.Do(func() { close(release) })
		}
	})
	var listed *partialCall
	for i := range calls {
		if _, ok := calls[i].res["repos"]; ok {
			listed = &calls[i]
			break
		}
	}
	if listed == nil || !slices.Contains(listed.loading, "host") || !slices.Contains(listed.loading, "image") ||
		!slices.Contains(listed.loading, "blockedBy") || !strings.Contains(string(listed.res["repos"]), `"repo":"a/one"`) {
		t.Fatalf("partials = %+v", calls)
	}
	if string(calls[0].res["user"]) != `"me"` || slices.Contains(calls[0].loading, "user") {
		t.Errorf("first partial = %v %v", calls[0].loading, keys(calls[0].res))
	}
	// The execution flag waits for the runner report.
	for _, c := range calls {
		if r, ok := c.res["nibfinRepos"]; ok && slices.Contains(c.loading, "host") && !strings.Contains(string(r), `"execution":null`) {
			t.Errorf("nibfinRepos before host = %s", r)
		}
	}
	if got := res["nibfinRepos"].([]NibfinRepo); len(got) != 1 || got[0].Execution == nil || !*got[0].Execution {
		t.Errorf("nibfinRepos = %+v", got)
	}
	for _, k := range []string{"user", "repos", "truncated", "githubHost", "latestImage"} {
		if !sameJSON(t, res[k], without[k]) {
			t.Errorf("%s = %#v, without a sink %#v", k, res[k], without[k])
		}
	}
}

func TestPartialCancelled(t *testing.T) {
	setup(t, repoRoutes())
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	returned := false
	ctx = WithPartial(ctx, func(op.Result, []string) {
		mu.Lock()
		defer mu.Unlock()
		if returned {
			t.Error("a partial after ReadBack returned")
		}
		cancel()
	})
	_, _, err := op.ReadBack(ctx, New(op.RepoParams{Repo: "github.com/o/r"}))
	mu.Lock()
	returned = true
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}
