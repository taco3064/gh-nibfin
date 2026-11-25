package op

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// fakeReg is a runner registration on the fake GitHub.
type fakeReg struct {
	ghRunner
	repo string
}

func (g *fakeRunnerAPI) listRegs(repo string) []ghRunner {
	if g.lists == nil {
		g.lists = map[string]int{}
	}
	g.lists[repo]++
	out := []ghRunner{}
	for _, r := range g.regs {
		if r.repo == repo {
			out = append(out, r.ghRunner)
		}
	}
	return out
}

func (g *fakeRunnerAPI) reg(repo string, id int64) *fakeReg {
	for _, r := range g.regs {
		if r.repo == repo && r.ID == id {
			return r
		}
	}
	return nil
}

func (g *fakeRunnerAPI) RemoveCustomLabels(_ context.Context, repo string, id int64) error {
	r := g.reg(repo, id)
	if r == nil {
		return nil
	}
	g.host.events = append(g.host.events, "labels "+r.Name)
	var keep []ghLabel
	for _, l := range r.Labels {
		if l.Type != "custom" {
			keep = append(keep, l)
		}
	}
	r.Labels = keep
	if g.onStrip != nil {
		g.onStrip()
	}
	return nil
}

func (g *fakeRunnerAPI) DeleteRunner(_ context.Context, repo string, id int64) error {
	if g.deleteErr != nil {
		return g.deleteErr
	}
	for i, r := range g.regs {
		if r.repo == repo && r.ID == id {
			g.host.events = append(g.host.events, "delete "+r.Name)
			if !g.keepRegs {
				g.regs = append(g.regs[:i], g.regs[i+1:]...)
			}
			return nil
		}
	}
	// Gone already: the REST client turns GitHub's 404 into success.
	return nil
}

// hostName is the name runner add gives a slot on the default probe's host.
func hostName(slot string) string {
	return "my-host-01-" + shaHex("gh-nibfin runner\n0123abcd")[:8] + "-" + slot
}

func labelled(custom ...string) []ghLabel {
	ls := []ghLabel{{"self-hosted", "read-only"}, {"Linux", "read-only"}, {"X64", "read-only"}}
	for _, c := range custom {
		ls = append(ls, ghLabel{c, "custom"})
	}
	return ls
}

func reg(id int64, name, repo string, busy bool) *fakeReg {
	return &fakeReg{ghRunner{ID: id, Name: name, Status: "online", Busy: busy, Labels: labelled("nibfin", "nibfin-42-claude")}, repo}
}

func done(name, url string) *fakeSlot {
	return &fakeSlot{configured: true, unit: true, enabled: true, active: true, name: name, url: url}
}

// removeHost is a host with three claude runners (claude-1 busy) and one
// codex runner, all serving github.com/o/r.
func removeHost(t *testing.T) (*fakeHost, *fakeRunnerAPI) {
	t.Helper()
	f := &fakeHost{slots: map[string]*fakeSlot{
		"claude-1": done(hostName("claude-1"), "https://github.com/o/r"),
		"claude-2": done(hostName("claude-2"), "https://github.com/o/r"),
		"claude-3": done(hostName("claude-3"), "https://github.com/o/r"),
		"codex-1":  done(hostName("codex-1"), "https://github.com/o/r"),
	}, volumes: map[string]bool{"claude": true, "codex": true}}
	fx := useRunnerHost(t, f)
	fx.gh.regs = []*fakeReg{
		reg(1, hostName("claude-1"), "github.com/o/r", true),
		reg(2, hostName("claude-2"), "github.com/o/r", false),
		reg(3, hostName("claude-3"), "github.com/o/r", false),
		reg(4, hostName("codex-1"), "github.com/o/r", false),
		// Another host's runner of the same slot.
		reg(5, "other-host-1234abcd-claude-1", "github.com/o/r", false),
	}
	return f, fx.gh
}

func resultJSON(t *testing.T, r Result) map[string]any {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRunnerRemoveAll(t *testing.T) {
	f, gh := removeHost(t)
	c := &yes{}
	p, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), c)
	if err != nil {
		t.Fatalf("Run: %v (events %v)", err, f.events)
	}
	want := []string{"remove runner claude-2", "remove runner claude-3", "remove runner claude-1"}
	if !reflect.DeepEqual(steps(p), want) || !p.Confirm || p.ReadOnly || p.Repo != "github.com/o/r" || len(c.plans) != 1 {
		t.Fatalf("plan = %v, %+v", steps(p), p)
	}
	if d := p.Changes()[2].Detail; d != hostName("claude-1")+", busy: waits for its Job to end" {
		t.Errorf("busy detail = %q", d)
	}
	if d := p.Changes()[0].Detail; d != hostName("claude-2")+", idle" {
		t.Errorf("idle detail = %q", d)
	}
	// Every label goes before any stop; the busy runner goes last and its
	// stop requires a worker.
	wantEvents := []string{
		"labels " + hostName("claude-2"), "labels " + hostName("claude-3"), "labels " + hostName("claude-1"),
		"stop claude-2 0", "delete " + hostName("claude-2"), "purge claude-2",
		"stop claude-3 0", "delete " + hostName("claude-3"), "purge claude-3",
		"stop claude-1 1", "delete " + hostName("claude-1"), "purge claude-1",
	}
	if !reflect.DeepEqual(f.events, wantEvents) {
		t.Errorf("events = %q\nwant %q", f.events, wantEvents)
	}
	for _, s := range f.shells {
		switch s.script {
		case "stop":
			if slot := strings.TrimSuffix(strings.TrimPrefix(s.args[2], "nibfin-runner-"), ".service"); s.args[0] != slotDir+"/"+slot || s.args[1] != unitDir || !strings.HasPrefix(slot, "claude-") {
				t.Errorf("stop args = %q", s.args)
			}
		case "purge":
			if s.args[0] != slotDir || s.args[2] != unitDir {
				t.Errorf("purge args = %q", s.args)
			}
		}
	}
	if len(gh.regs) != 2 || gh.regs[0].ID != 4 || gh.regs[1].ID != 5 {
		t.Errorf("registrations left: %+v", gh.regs)
	}
	m := resultJSON(t, r)
	if m["remaining"] != float64(0) || m["provider"] != "claude" || m["host"] != "fake" ||
		!reflect.DeepEqual(m["removed"], []any{"claude-2", "claude-3", "claude-1"}) {
		t.Errorf("result = %v", m)
	}
	if rs := m["runners"].([]any); len(rs) != 1 || rs[0].(map[string]any)["slot"] != "codex-1" || rs[0].(map[string]any)["listed"] != true {
		t.Errorf("runners = %v", m["runners"])
	}
	if l := m["login"].(map[string]any); l["volume"] != "kept" || !f.volumes["claude"] {
		t.Errorf("login = %v", l)
	}
}

func TestRunnerRemoveCount(t *testing.T) {
	f, gh := removeHost(t)
	// A slot a crashed add left without .runner, registered under the name
	// runner add gives it, goes first.
	f.slots["claude-4"] = &fakeSlot{}
	gh.regs = append(gh.regs, reg(6, hostName("claude-4"), "github.com/o/r", false))
	o := RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2, DeleteLogin: true})
	p, err := o.Plan(context.Background())
	want := []string{"start fake", "remove runner claude-4", "remove runner claude-2", "keep volume nibfin-home-claude"}
	if err != nil || !reflect.DeepEqual(allSteps(p), want) {
		t.Fatalf("plan = %v, %v", allSteps(p), err)
	}
	if d := p.Changes()[0].Detail; d != hostName("claude-4")+", stale" {
		t.Errorf("stale detail = %q", d)
	}
	if d := p.Steps[3].Detail; d != "2 claude runners remain" {
		t.Errorf("keep detail = %q", d)
	}
	if len(f.events) != 0 {
		t.Errorf("plan changed something: %v", f.events)
	}
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	r, err := o.ReadBack(context.Background(), p)
	if err != nil {
		t.Fatalf("ReadBack: %v", err)
	}
	// The busy runner keeps its labels.
	for _, e := range f.events {
		if strings.Contains(e, "claude-1") || strings.Contains(e, "claude-3") || strings.HasPrefix(e, "rm volume") {
			t.Errorf("touched %s", e)
		}
	}
	if !gh.reg("github.com/o/r", 1).hasCustomLabel("nibfin") {
		t.Error("the busy runner lost its labels")
	}
	m := resultJSON(t, r)
	if m["remaining"] != float64(2) || !reflect.DeepEqual(m["removed"], []any{"claude-4", "claude-2"}) {
		t.Errorf("result = %v", m)
	}
	if l := m["login"].(map[string]any); l["volume"] != "kept" || l["reason"] != "2 claude runners remain" {
		t.Errorf("login = %v", l)
	}

	// More than there are is refused with the current number.
	_, err = RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 5}).Plan(context.Background())
	if Code(err) != CodeInvalidArgument || err.Error() != "only 2 claude runners on fake" {
		t.Errorf("count 5: %v", err)
	}
}

func TestRunnerRemoveNoRunners(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": done(hostName("claude-1"), "https://github.com/o/r")},
		volumes: map[string]bool{"codex": true}}
	useRunnerHost(t, f)
	if _, err := RunnerRemove(RunnerRemoveParams{Provider: "codex"}).Plan(context.Background()); Code(err) != CodeNoRunners {
		t.Errorf("no runners: %v", err)
	}
	if _, err := RunnerRemove(RunnerRemoveParams{Provider: "codex", Count: 1, DeleteLogin: true}).Plan(context.Background()); Code(err) != CodeInvalidArgument || !strings.Contains(err.Error(), "only 0 codex") {
		t.Errorf("count 1 of 0: %v", err)
	}
	p, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "codex", DeleteLogin: true}), &yes{})
	if err != nil || !reflect.DeepEqual(steps(p), []string{"delete volume nibfin-home-codex"}) || p.ReadOnly {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	if !reflect.DeepEqual(f.events, []string{"rm volume nibfin-home-codex"}) || f.volumes["codex"] {
		t.Errorf("events = %v", f.events)
	}
	if l := resultJSON(t, r)["login"].(map[string]any); l["volume"] != "deleted" {
		t.Errorf("login = %v", l)
	}

	// Again: nothing left to do.
	f.events = nil
	c := &yes{}
	p, r, err = Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "codex", DeleteLogin: true}), c)
	if err != nil || !p.ReadOnly || len(c.plans) != 0 || len(f.events) != 0 || !reflect.DeepEqual(allSteps(p), []string{"start fake", "keep volume nibfin-home-codex"}) || p.Steps[1].Detail != "no such volume" {
		t.Errorf("rerun: %v, %+v, events %v", err, p, f.events)
	}
	if l := resultJSON(t, r)["login"].(map[string]any); l["volume"] != "absent" {
		t.Errorf("login = %v", l)
	}
}

func TestRunnerRemoveDeleteLogin(t *testing.T) {
	f, _ := removeHost(t)
	_, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", DeleteLogin: true}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The volume goes after the last runner, the waited-for busy one too.
	if n := len(f.events); n == 0 || f.events[n-1] != "rm volume nibfin-home-claude" || f.events[n-2] != "purge claude-1" {
		t.Errorf("events = %v", f.events)
	}
	if f.volumes["claude"] || !f.volumes["codex"] {
		t.Errorf("volumes = %v", f.volumes)
	}
	if l := resultJSON(t, r)["login"].(map[string]any); l["volume"] != "deleted" {
		t.Errorf("login = %v", l)
	}

	// A volume in use is kept: podman refuses it.
	f = &fakeHost{slots: map[string]*fakeSlot{}, volumes: map[string]bool{"codex": true}, rmExit: 2}
	useRunnerHost(t, f)
	_, _, err = Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "codex", DeleteLogin: true}), &yes{})
	if Code(err) != CodePodmanFailed || !f.volumes["codex"] {
		t.Errorf("in use: %v", err)
	}
}

func TestRunnerRemoveMatching(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{
		"claude-1": done("a-claude-1", "https://github.com/o/one"),
		"claude-2": done("A-CLAUDE-2", "https://ghe.example/o/two"),
		"claude-3": {configured: true, name: "c-claude-3", url: "https://github.com/o/one"},
	}}
	fx := useRunnerHost(t, f)
	fx.gh.regs = []*fakeReg{
		reg(1, "a-claude-1", "github.com/o/one", false),
		reg(1, "a-claude-2", "ghe.example/o/two", false),
		// The same names in another repository are not this host's.
		reg(7, "a-claude-2", "github.com/o/one", false),
		reg(8, "c-claude-3", "ghe.example/o/two", false),
	}
	p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (%v)", err, f.events)
	}
	// claude-3 (no unit) is stale and not registered in its repository.
	want := []string{"remove runner claude-3", "remove runner claude-1", "remove runner claude-2"}
	if !reflect.DeepEqual(steps(p), want) || p.Repo != "" || p.Changes()[0].Detail != "c-claude-3 (not registered), stale" {
		t.Errorf("plan = %v %+v", steps(p), p)
	}
	if len(fx.gh.regs) != 2 || fx.gh.regs[0].ID != 7 || fx.gh.regs[1].ID != 8 {
		t.Errorf("registrations left: %+v", fx.gh.regs)
	}
	if fx.gh.lists["github.com/o/one"] == 0 || fx.gh.lists["ghe.example/o/two"] == 0 {
		t.Errorf("lists = %v", fx.gh.lists)
	}
	for _, h := range *fx.hosts {
		if h != "github.com" && h != "ghe.example" {
			t.Errorf("gh host %q", h)
		}
	}
}

func TestRunnerRemoveReclassifiesAfterLabels(t *testing.T) {
	f, gh := removeHost(t)
	// A Job reaches claude-2 just before its labels go.
	gh.onStrip = func() { gh.reg("github.com/o/r", 2).Busy = true }
	_, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var stops []string
	for _, e := range f.events {
		if strings.HasPrefix(e, "stop ") {
			stops = append(stops, e)
		}
	}
	if !reflect.DeepEqual(stops, []string{"stop claude-3 0", "stop claude-2 1"}) {
		t.Errorf("stops = %v", stops)
	}
}

func TestRunnerRemoveBusyGuard(t *testing.T) {
	f, gh := removeHost(t)
	f.stopExit = map[string]int{"claude-1": busyExit}
	_, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", DeleteLogin: true}), &yes{})
	if Code(err) != CodeRunnerBusy || !strings.Contains(err.Error(), "claude-1") {
		t.Fatalf("err = %v", err)
	}
	for _, e := range f.events {
		if e == "delete "+hostName("claude-1") || e == "purge claude-1" || strings.HasPrefix(e, "rm volume") {
			t.Errorf("went on: %v", f.events)
		}
	}
	if s := f.slots["claude-1"]; s == nil || !s.active {
		t.Error("the busy runner was stopped")
	}
	if r := gh.reg("github.com/o/r", 1); r == nil || r.customLabels() {
		t.Errorf("busy runner = %+v", r)
	}
	if f.slots["claude-2"] != nil || f.slots["claude-3"] != nil {
		t.Error("the idle runners were not removed")
	}

	// The next run finishes it first: it is stale now.
	f.stopExit = nil
	f.events = nil
	p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil || !reflect.DeepEqual(steps(p), []string{"remove runner claude-1"}) || !strings.Contains(p.Changes()[0].Detail, "stale: waits for its Job to end") {
		t.Errorf("rerun: %v %+v", err, p)
	}
}

func TestRunnerRemoveFailures(t *testing.T) {
	f, gh := removeHost(t)
	gh.deleteErr = Errorf(CodeGitHubAPIFailed, "delete runner: HTTP 403: Must have admin rights")
	_, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeGitHubAPIFailed {
		t.Errorf("delete: %v", err)
	}
	if f.slots["claude-2"] == nil {
		t.Error("the slot was purged before its registration was deleted")
	}

	f, _ = removeHost(t)
	f.shellExit = map[string]int{"stop": 1}
	f.shellErr = map[string]string{"stop": "tail: unrecognized option"}
	_, _, err = Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerRemoveFailed || !strings.Contains(err.Error(), "stop claude-2") || !strings.Contains(err.Error(), "tail: unrecognized option") {
		t.Errorf("stop: %v", err)
	}

	f, _ = removeHost(t)
	f.vm = true
	f.shellExit = map[string]int{"purge": 125}
	_, _, err = Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodePodmanFailed {
		t.Errorf("ssh: %v", err)
	}
}

func TestRunnerRemoveReadBackNotApplied(t *testing.T) {
	f, _ := removeHost(t)
	f.purgeNoop = true
	_, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "claude-2 (still on the host)") || r == nil {
		t.Errorf("local: %v", err)
	}

	f, gh := removeHost(t)
	gh.keepRegs = true
	_, _, err = Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "claude-2 (still registered on GitHub)") {
		t.Errorf("listed: %v", err)
	}

	// A planned volume deletion that left the volume.
	f = &fakeHost{slots: map[string]*fakeSlot{}, volumes: map[string]bool{"codex": true}}
	useRunnerHost(t, f)
	o := RunnerRemove(RunnerRemoveParams{Provider: "codex", DeleteLogin: true})
	p, _ := o.Plan(context.Background())
	o.(*runnerRemove).deleteVolume = true
	if _, err := o.ReadBack(context.Background(), p); Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "nibfin-home-codex") {
		t.Errorf("volume: %v", err)
	}
}

func TestRunnerRemoveStoppedMachine(t *testing.T) {
	f, _ := removeHost(t)
	f.stopped, f.vm = true, true
	o := RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2, DeleteLogin: true})
	p, err := o.Plan(context.Background())
	want := []string{"start fake", "remove runners claude ×2", "delete volume nibfin-home-claude"}
	if err != nil || !reflect.DeepEqual(steps(p), want) || !p.Confirm {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	if len(f.calls) != 0 || len(f.shells) != 0 {
		t.Errorf("plan touched a stopped machine: %v %v", f.calls, f.shells)
	}
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// After the start: the two idle runners; one remains, so the volume stays.
	if f.starts != 1 || f.slots["claude-2"] != nil || f.slots["claude-3"] != nil || f.slots["claude-1"] == nil || !f.volumes["claude"] {
		t.Errorf("starts %d, events %v", f.starts, f.events)
	}
	if _, err := o.ReadBack(context.Background(), p); err != nil {
		t.Errorf("ReadBack: %v", err)
	}
	p, _ = RunnerRemove(RunnerRemoveParams{Provider: "codex"}).Plan(context.Background())
	if !reflect.DeepEqual(steps(p), []string{"remove runner codex-1"}) {
		t.Errorf("plan = %v", steps(p))
	}
	f.stopped = true
	p, _ = RunnerRemove(RunnerRemoveParams{Provider: "codex"}).Plan(context.Background())
	if !reflect.DeepEqual(steps(p), []string{"start fake", "remove runners codex all"}) {
		t.Errorf("plan = %v", steps(p))
	}
}

func TestRunnerRemoveInvalid(t *testing.T) {
	removeHost(t)
	for _, p := range []RunnerRemoveParams{{Provider: "gemini"}, {Provider: "claude", Count: -1}} {
		if _, err := RunnerRemove(p).Plan(context.Background()); Code(err) != CodeInvalidArgument {
			t.Errorf("%+v: %v", p, err)
		}
	}
	f, _ := removeHost(t)
	for _, s := range []Step{
		{Action: actRemoveRunner, Target: "codex-1"},
		{Action: actRemoveRunner, Target: "claude-9"},
		{Action: actRemoveRunner, Target: "../claude-1"},
		{Action: actDeleteVolume, Target: "nibfin-home-codex"},
	} {
		err := RunnerRemove(RunnerRemoveParams{Provider: "claude"}).Apply(context.Background(), Plan{Steps: []Step{s}})
		if Code(err) != CodeInvalidArgument {
			t.Errorf("%v: %v", s, err)
		}
	}
	if len(f.events) != 0 {
		t.Errorf("events = %v", f.events)
	}
}

func TestRunnerRemoveParamsJSON(t *testing.T) {
	b, err := json.Marshal(RunnerRemoveParams{Provider: "claude", Count: 2, DeleteLogin: true})
	if err != nil || string(b) != `{"provider":"claude","count":2,"deleteLogin":true}` {
		t.Errorf("json = %s, %v", b, err)
	}
}

// TestRemoveRunnerREST checks the two REST calls against a fake GitHub: a
// runner that is gone already (404) is success.
func TestRemoveRunnerREST(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v3/repos/o/r/actions/runners/1/labels":
			io.WriteString(w, `{"total_count":1,"labels":[{"id":1,"name":"self-hosted","type":"read-only"}]}`)
		case "/api/v3/repos/o/r/actions/runners/1":
			w.WriteHeader(http.StatusNoContent)
		case "/api/v3/repos/o/r/actions/runners/3":
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message":"Validation Failed"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c, err := api.NewRESTClient(api.ClientOptions{Host: "ghe.example", AuthToken: "x", Transport: rewrite{u}})
	if err != nil {
		t.Fatal(err)
	}
	g := restGitHub{c}
	ctx := context.Background()
	if err := g.RemoveCustomLabels(ctx, "ghe.example/o/r", 1); err != nil {
		t.Errorf("labels: %v", err)
	}
	if err := g.DeleteRunner(ctx, "ghe.example/o/r", 1); err != nil {
		t.Errorf("delete: %v", err)
	}
	if err := g.RemoveCustomLabels(ctx, "ghe.example/o/r", 2); err != nil {
		t.Errorf("labels 404: %v", err)
	}
	if err := g.DeleteRunner(ctx, "ghe.example/o/r", 2); err != nil {
		t.Errorf("delete 404: %v", err)
	}
	if err := g.DeleteRunner(ctx, "ghe.example/o/r", 3); Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "422") {
		t.Errorf("delete 422: %v", err)
	}
	want := []string{
		"DELETE /api/v3/repos/o/r/actions/runners/1/labels", "DELETE /api/v3/repos/o/r/actions/runners/1",
		"DELETE /api/v3/repos/o/r/actions/runners/2/labels", "DELETE /api/v3/repos/o/r/actions/runners/2",
		"DELETE /api/v3/repos/o/r/actions/runners/3",
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("requests = %q", seen)
	}
}

// rewrite sends every request to the test server.
type rewrite struct{ u *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.u.Scheme, r.u.Host
	return http.DefaultTransport.RoundTrip(req)
}

// TestFakeWorker is the body of the fake Runner.Worker processes of
// TestRemoveScriptsUnderSh: a copy of the test binary that sleeps.
func TestFakeWorker(t *testing.T) {
	d, err := time.ParseDuration(os.Getenv("GO_WANT_FAKE_WORKER"))
	if err != nil {
		t.Skip("not a fake worker")
	}
	time.Sleep(d)
}

// TestRemoveScriptsUnderSh runs the real stop and purge scripts against a
// directory tree, with a stand-in for systemctl and short-lived processes
// run from a copy of the test binary named Runner.Worker as the Jobs. (A
// copy of sleep is no fake worker where sleep is a multi-call binary that
// dispatches on its name, as uutils coreutils does.)
func TestRemoveScriptsUnderSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts run on the Linux runner host")
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip("no test binary")
	}
	if err := exec.Command("tail", "--pid=1", "--version").Run(); err != nil {
		t.Skip("no tail --pid")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	pids := filepath.Join(root, "pids")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(pids, 0o755)
	log := filepath.Join(root, "systemctl.log")
	// systemctl records its arguments, and "alive" for each worker still
	// running when it is called.
	os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nfor f in \"$PIDS\"/*; do [ -f \"$f\" ] && kill -0 \"$(cat \"$f\")\" 2>/dev/null && echo alive >> \"$LOG\"; done\necho \"$*\" >> \"$LOG\"\n"), 0o755)
	h := &localHost{env: []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "LOG=" + log, "PIDS=" + pids}}
	ctx := context.Background()

	dir := filepath.Join(root, "data", "nibfin", "runners")
	units := filepath.Join(root, "units")
	slot := filepath.Join(dir, "claude-1")
	os.MkdirAll(filepath.Join(slot, "bin"), 0o755)
	os.MkdirAll(units, 0o755)
	unit := runners.UnitName("claude-1")
	os.WriteFile(filepath.Join(units, unit), []byte("[Unit]\n"), 0o644)
	worker := filepath.Join(slot, "bin", "Runner.Worker")
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worker, b, 0o755); err != nil {
		t.Fatal(err)
	}
	start := func(secs string) error {
		c := exec.Command(worker, "-test.run=^TestFakeWorker$")
		c.Env = append(os.Environ(), "GO_WANT_FAKE_WORKER="+secs+"s")
		if err := c.Start(); err != nil {
			return err
		}
		os.WriteFile(filepath.Join(pids, secs), []byte(strconv.Itoa(c.Process.Pid)), 0o644)
		// Reaped at once, as the runner listener does.
		go c.Wait()
		return nil
	}
	stop := func(require string) (int, error) {
		_, exit, err := hostScript(ctx, h, CodeRunnerRemoveFailed, "stop", stopScript, nil, "", slot, units, unit, require)
		return exit, err
	}
	readLog := func() string {
		b, _ := os.ReadFile(log)
		os.Remove(log)
		return string(b)
	}

	// No worker: busy on GitHub stops nothing; otherwise it stops at once.
	if exit, err := stop("1"); exit != busyExit || Code(err) != CodeRunnerRemoveFailed || !strings.Contains(err.Error(), "no Runner.Worker") {
		t.Errorf("guard: %d %v", exit, err)
	}
	if l := readLog(); l != "" {
		t.Errorf("guard stopped: %q", l)
	}
	if _, err := stop("0"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if l := readLog(); l != "--user disable --now "+unit+"\n" {
		t.Errorf("systemctl = %q", l)
	}

	// A worker, and a second one that starts while the first is waited for:
	// the stop comes only after both ended.
	if err := start("1"); err != nil {
		t.Fatal(err)
	}
	second := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		second <- start("2")
	}()
	began := time.Now()
	_, err = stop("1")
	if err2 := <-second; err2 != nil {
		t.Fatalf("second worker: %v", err2)
	}
	if err != nil {
		t.Fatalf("stop with workers: %v", err)
	}
	if took := time.Since(began); took < 2*time.Second {
		t.Errorf("stopped after %v, before the second worker ended", took)
	}
	if l := readLog(); l != "--user disable --now "+unit+"\n" {
		t.Errorf("systemctl = %q", l)
	}

	// No unit file: nothing to stop.
	os.Remove(filepath.Join(units, unit))
	if _, err := stop("0"); err != nil || readLog() != "" {
		t.Errorf("no unit: %v", err)
	}

	// The purge refuses anything but <provider>-<n> in .../nibfin/runners.
	os.WriteFile(filepath.Join(units, unit), []byte("[Unit]\n"), 0o644)
	purge := func(args ...string) error {
		_, _, err := hostScript(ctx, h, CodeRunnerRemoveFailed, "purge", purgeScript, nil, "", args...)
		return err
	}
	for _, args := range [][]string{
		{filepath.Join(root, "data"), "nibfin", units},
		{dir, "..", units},
		{dir, "claude-1/..", units},
		{dir, "claude-0", units},
		{dir, "claude", units},
		{dir, "claude-x-1", units},
		{dir, "claude-1", "units"},
		{dir + "/../../nibfin/runners", "claude-1", units},
	} {
		if err := purge(args...); Code(err) != CodeRunnerRemoveFailed || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("%q: %v", args, err)
		}
	}
	if _, err := os.Stat(slot); err != nil || readLog() != "" {
		t.Fatalf("a refused purge removed something: %v", err)
	}
	if err := purge(dir, "claude-1", units); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(slot); !os.IsNotExist(err) {
		t.Errorf("slot left: %v", err)
	}
	if _, err := os.Stat(filepath.Join(units, unit)); !os.IsNotExist(err) {
		t.Errorf("unit left: %v", err)
	}
	if l := readLog(); l != "--user daemon-reload\n" {
		t.Errorf("systemctl = %q", l)
	}
	// Repeat-safe.
	if err := purge(dir, "claude-1", units); err != nil {
		t.Errorf("purge again: %v", err)
	}
}
