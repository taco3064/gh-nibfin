package op

import (
	"context"
	"encoding/json"
	"errors"
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
	if err := g.labelErr[id]; err != nil {
		return err
	}
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
			if r.Busy {
				// GitHub refuses to delete a runner running a Job.
				g.host.events = append(g.host.events, "refused "+r.Name)
				return runningJobError(Errorf(CodeGitHubAPIFailed, "delete runner %d from %s: HTTP 422: Bad request - Runner %s is currently running a job and cannot be deleted.", id, repo, r.Name))
			}
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
	// The drain waits for the Jobs on the host: a busy runner is idle after.
	f.onDrain = func(slot string) {
		if r := fx.gh.byName(hostName(slot)); r != nil {
			r.Busy = false
		}
	}
	fakeClock(t, func(int) error {
		t.Errorf("unexpected wait (events %q)", f.events)
		return context.Canceled
	})
	return f, fx.gh
}

func (g *fakeRunnerAPI) byName(name string) *fakeReg {
	for _, r := range g.regs {
		if r.Name == name {
			return r
		}
	}
	return nil
}

// fakeClock replaces the wait between the re-reads of a runner whose delete
// GitHub refuses: tick gets the number of the wait, from 1, and its error
// ends the wait.
func fakeClock(t *testing.T, tick func(n int) error) *[]time.Duration {
	t.Helper()
	old := removeWaitSleep
	t.Cleanup(func() { removeWaitSleep = old })
	waits := &[]time.Duration{}
	removeWaitSleep = func(_ context.Context, d time.Duration) error {
		*waits = append(*waits, d)
		return tick(len(*waits))
	}
	return waits
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
	// Every label goes before any removal; the busy runner goes last. A
	// registered runner is drained, deleted on GitHub, then stopped.
	wantEvents := []string{
		"labels " + hostName("claude-2"), "labels " + hostName("claude-3"), "labels " + hostName("claude-1"),
		"drain claude-2", "delete " + hostName("claude-2"), "stop claude-2", "purge claude-2",
		"drain claude-3", "delete " + hostName("claude-3"), "stop claude-3", "purge claude-3",
		"drain claude-1", "delete " + hostName("claude-1"), "stop claude-1", "purge claude-1",
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
	var drains []string
	for _, e := range f.events {
		if strings.HasPrefix(e, "drain ") {
			drains = append(drains, e)
		}
	}
	if !reflect.DeepEqual(drains, []string{"drain claude-3", "drain claude-2"}) {
		t.Errorf("drains = %v", drains)
	}
}

// progress records the progress events of an Apply under the returned ctx.
func progress(ctx context.Context) (context.Context, *[]Event) {
	evs := &[]Event{}
	return WithProgress(ctx, func(e Event) { *evs = append(*evs, e) }), evs
}

// waits returns the waiting events: started events with a message.
func waits(evs []Event) []string {
	var out []string
	for _, e := range evs {
		if e.Phase == PhaseStarted && e.Message != "" {
			out = append(out, e.Target+": "+e.Message)
		}
	}
	return out
}

func TestRunnerRemoveWaitsForBusyJob(t *testing.T) {
	f, gh := removeHost(t)
	// GitHub reports claude-1 busy, but no worker of it shows on the host
	// yet: the drain finds nothing, and GitHub refuses the delete until the
	// Job ends after the second wait.
	f.onDrain = nil
	clock := fakeClock(t, func(n int) error {
		if n == 2 {
			gh.reg("github.com/o/r", 1).Busy = false
		}
		return nil
	})
	ctx, evs := progress(context.Background())
	_, r, err := Run(ctx, RunnerRemove(RunnerRemoveParams{Provider: "claude", DeleteLogin: true}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (events %q)", err, f.events)
	}
	// The idle ones go first; the busy one is deleted only once GitHub
	// reports it no longer busy, then stopped and purged.
	busy := hostName("claude-1")
	want := []string{
		"labels " + hostName("claude-2"), "labels " + hostName("claude-3"), "labels " + busy,
		"drain claude-2", "delete " + hostName("claude-2"), "stop claude-2", "purge claude-2",
		"drain claude-3", "delete " + hostName("claude-3"), "stop claude-3", "purge claude-3",
		"drain claude-1", "refused " + busy,
		"drain claude-1", "refused " + busy,
		"drain claude-1", "delete " + busy, "stop claude-1", "purge claude-1",
		"rm volume nibfin-home-claude",
	}
	if !reflect.DeepEqual(f.events, want) {
		t.Errorf("events = %q\nwant %q", f.events, want)
	}
	if !reflect.DeepEqual(*clock, []time.Duration{removeWaitInterval, removeWaitInterval}) {
		t.Errorf("waits = %v", *clock)
	}
	// One waiting line, from the re-read after the labels.
	if w := waits(*evs); !reflect.DeepEqual(w, []string{"claude-1: waiting for its Job to end: " + busy + " in github.com/o/r"}) {
		t.Errorf("waiting = %q", w)
	}
	for _, e := range *evs {
		if e.Phase == PhaseFailed {
			t.Errorf("failed event %+v", e)
		}
	}
	if m := resultJSON(t, r); m["remaining"] != float64(0) {
		t.Errorf("result = %v", m)
	}
}

func TestRunnerRemoveRefusedDeleteWaits(t *testing.T) {
	f, gh := removeHost(t)
	// claude-2 is idle when chosen, but GitHub gives it a Job right after
	// the drain: its delete is refused, and the Job ends after one wait.
	given := false
	f.onDrain = func(slot string) {
		if slot == "claude-2" && !given {
			given = true
			gh.reg("github.com/o/r", 2).Busy = true
		}
	}
	fakeClock(t, func(int) error {
		gh.reg("github.com/o/r", 2).Busy = false
		return nil
	})
	ctx, evs := progress(context.Background())
	_, r, err := Run(ctx, RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (events %q)", err, f.events)
	}
	name := hostName("claude-2")
	want := []string{
		"labels " + name, "labels " + hostName("claude-3"),
		"drain claude-2", "refused " + name, "drain claude-2", "delete " + name, "stop claude-2", "purge claude-2",
		"drain claude-3", "delete " + hostName("claude-3"), "stop claude-3", "purge claude-3",
	}
	if !reflect.DeepEqual(f.events, want) {
		t.Errorf("events = %q\nwant %q", f.events, want)
	}
	// The service ran until GitHub accepted the delete.
	if w := waits(*evs); !reflect.DeepEqual(w, []string{"claude-2: waiting for its Job to end: " + name + " in github.com/o/r"}) {
		t.Errorf("waiting = %q", w)
	}
	if m := resultJSON(t, r); m["remaining"] != float64(1) || !reflect.DeepEqual(m["removed"], []any{"claude-2", "claude-3"}) {
		t.Errorf("result = %v", m)
	}
}

func TestRunnerRemoveWaitFailsOneItem(t *testing.T) {
	f, gh := removeHost(t)
	// claude-2 gets a Job after its drain and then goes offline with it.
	idle := f.onDrain
	f.onDrain = func(slot string) {
		idle(slot)
		if slot == "claude-2" {
			r := gh.reg("github.com/o/r", 2)
			r.Busy, r.Status = true, "offline"
		}
	}
	ctx, evs := progress(context.Background())
	_, r, err := Run(ctx, RunnerRemove(RunnerRemoveParams{Provider: "claude", DeleteLogin: true}), &yes{})
	// The item keeps the cause: GitHub's refusal, with its code.
	if Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "offline while its Job still runs") ||
		!strings.Contains(err.Error(), "is currently running a job and cannot be deleted") {
		t.Fatalf("err = %v", err)
	}
	// The other items are still removed; the volume stays.
	if f.slots["claude-2"] == nil || f.slots["claude-3"] != nil || f.slots["claude-1"] != nil || !f.volumes["claude"] {
		t.Errorf("events = %q", f.events)
	}
	for _, e := range f.events {
		if e == "stop claude-2" || e == "purge claude-2" || strings.HasPrefix(e, "rm volume") {
			t.Errorf("went on with %s: %q", e, f.events)
		}
	}
	var failed []string
	for _, e := range *evs {
		if e.Phase == PhaseFailed {
			failed = append(failed, e.Target+" "+e.Code)
		}
	}
	if !reflect.DeepEqual(failed, []string{"claude-2 github_api_failed"}) {
		t.Errorf("failed = %q", failed)
	}
	// The read-back still runs: the remaining count and each item.
	if r == nil {
		t.Fatal("no read-back after the failure")
	}
	m := resultJSON(t, r)
	if m["remaining"] != float64(1) || !reflect.DeepEqual(m["removed"], []any{"claude-2", "claude-3", "claude-1"}) {
		t.Errorf("result = %v", m)
	}
	for _, it := range m["items"].([]any) {
		it := it.(map[string]any)
		if it["target"] == "claude-2" && (it["confirmed"] != false || it["reason"] != "still on the host and on GitHub") {
			t.Errorf("item = %v", it)
		}
		if it["target"] == "claude-3" && it["confirmed"] != true {
			t.Errorf("item = %v", it)
		}
	}

	// The next run finishes it once its Job is resolved: it is stale now.
	r2 := gh.reg("github.com/o/r", 2)
	r2.Busy, r2.Status = false, "online"
	f.onDrain, f.events = idle, nil
	p, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if err != nil || !reflect.DeepEqual(steps(p), []string{"remove runner claude-2"}) || f.slots["claude-2"] != nil {
		t.Errorf("rerun: %v %q %q", err, steps(p), f.events)
	}
}

func TestRunnerRemoveWaitReadFails(t *testing.T) {
	f, gh := removeHost(t)
	// The re-read during the wait fails after its retries.
	listErr := Errorf(CodeGitHubAPIFailed, "list the runners of github.com/o/r: HTTP 502: Bad Gateway")
	f.onDrain = func(slot string) {
		if slot == "claude-2" {
			gh.reg("github.com/o/r", 2).Busy = true
			gh.listErr = listErr
		}
	}
	_, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2}), &yes{})
	if Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "wait for the Job of "+hostName("claude-2")) || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
	if f.slots["claude-2"] == nil || f.slots["claude-3"] != nil {
		t.Errorf("events = %q", f.events)
	}
	// The read-back reads GitHub again; its failure marks the item unread.
	if r == nil {
		t.Fatal("no read-back after the failure")
	}
}

func TestRunnerRemoveCancelledWait(t *testing.T) {
	f, gh := removeHost(t)
	f.onDrain = func(slot string) {
		if slot == "claude-2" {
			gh.reg("github.com/o/r", 2).Busy = true
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeClock(t, func(int) error {
		cancel()
		return ctx.Err()
	})
	pctx, evs := progress(ctx)
	_, r, err := Run(pctx, RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2}), &yes{})
	if Code(err) != CodeCancelled || r != nil {
		t.Fatalf("err = %v, result %v", err, r)
	}
	// The open items stop: claude-3 is not removed.
	if f.slots["claude-3"] == nil || f.slots["claude-2"] == nil {
		t.Errorf("events = %q", f.events)
	}
	var failed []string
	for _, e := range *evs {
		if e.Phase == PhaseFailed {
			failed = append(failed, e.Target+" "+e.Code+" "+e.Message)
		}
	}
	if len(failed) != 2 || !strings.HasPrefix(failed[0], "claude-2 cancelled") || !strings.HasPrefix(failed[1], "claude-3 cancelled stopped before this item finished") {
		t.Errorf("failed = %q", failed)
	}
}

func TestRunnerRemoveOtherFailuresGoOn(t *testing.T) {
	// A refused label removal fails only its item; it is read back as
	// still there.
	f, gh := removeHost(t)
	gh.labelErr = map[int64]error{2: Errorf(CodeGitHubAPIFailed, "remove the custom labels of runner 2 in github.com/o/r: HTTP 403: Forbidden")}
	_, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2}), &yes{})
	if Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
	if f.slots["claude-2"] == nil || f.slots["claude-3"] != nil {
		t.Errorf("events = %q", f.events)
	}
	if m := resultJSON(t, r); m["remaining"] != float64(2) || !reflect.DeepEqual(m["removed"], []any{"claude-2", "claude-3"}) {
		t.Errorf("result = %v", r)
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
		case "/api/v3/repos/o/r/actions/runners/4":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"message":"Bad request - Runner h-claude-1 is currently running a job and cannot be deleted."}`)
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
	if err := g.DeleteRunner(ctx, "ghe.example/o/r", 3); Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "422") || errors.Is(err, errRunningJob) {
		t.Errorf("delete 422: %v", err)
	}
	// Only the refusal of a runner running a Job is recognised; it keeps its
	// code and GitHub's message.
	if err := g.DeleteRunner(ctx, "ghe.example/o/r", 4); !errors.Is(err, errRunningJob) || Code(err) != CodeGitHubAPIFailed ||
		err.Error() != "delete runner 4 from ghe.example/o/r: HTTP 422: Bad request - Runner h-claude-1 is currently running a job and cannot be deleted." {
		t.Errorf("delete running job: %v", err)
	}
	want := []string{
		"DELETE /api/v3/repos/o/r/actions/runners/1/labels", "DELETE /api/v3/repos/o/r/actions/runners/1",
		"DELETE /api/v3/repos/o/r/actions/runners/2/labels", "DELETE /api/v3/repos/o/r/actions/runners/2",
		"DELETE /api/v3/repos/o/r/actions/runners/3",
		"DELETE /api/v3/repos/o/r/actions/runners/4",
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
	stop := func() error {
		_, _, err := hostScript(ctx, h, CodeRunnerRemoveFailed, "stop", stopScript, nil, "", slot, units, unit)
		return err
	}
	drain := func() error {
		_, _, err := hostScript(ctx, h, CodeRunnerRemoveFailed, "drain", drainScript, nil, "", slot)
		return err
	}
	readLog := func() string {
		b, _ := os.ReadFile(log)
		os.Remove(log)
		return string(b)
	}

	// No worker: the drain returns and stops nothing; the stop stops at once.
	if err := drain(); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if l := readLog(); l != "" {
		t.Errorf("drain stopped: %q", l)
	}
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if l := readLog(); l != "--user disable --now "+unit+"\n" {
		t.Errorf("systemctl = %q", l)
	}

	// The drain waits for a worker and stops nothing.
	if err := start("1"); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if err := drain(); err != nil {
		t.Fatalf("drain with a worker: %v", err)
	}
	if took := time.Since(began); took < 700*time.Millisecond {
		t.Errorf("drained after %v, before the worker ended", took)
	}
	if l := readLog(); l != "" {
		t.Errorf("drain stopped: %q", l)
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
	began = time.Now()
	err = stop()
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
	if err := stop(); err != nil || readLog() != "" {
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

// collaboratorHost is removeHost for a gh login that writes to github.com/o/r
// but is not admin: every GitHub runner call fails if made.
func collaboratorHost(t *testing.T, perms repoPerms) (*fakeHost, *fakeRunnerAPI) {
	t.Helper()
	f, gh := removeHost(t)
	gh.perms = &perms
	gh.listErr = Errorf(CodeGitHubAPIFailed, "ListRunners must not be called")
	gh.deleteErr = Errorf(CodeGitHubAPIFailed, "DeleteRunner must not be called")
	return f, gh
}

const collaboratorNote = "in github.com/o/r stays: you are not admin of github.com/o/r. Its owner removes it under Settings → Actions → Runners (https://github.com/o/r/settings/actions/runners). GitHub also removes a runner that has been offline for more than 14 days."

func TestRunnerRemoveCollaborator(t *testing.T) {
	for _, tc := range []struct {
		name  string
		perms repoPerms
	}{
		{"write", repoPerms{Push: true}},
		// A repository the login cannot see reads as no permissions.
		{"invisible", repoPerms{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, gh := collaboratorHost(t, tc.perms)
			c := &yes{}
			p, r, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), c)
			if err != nil {
				t.Fatalf("Run: %v (events %v)", err, f.events)
			}
			want := []string{"remove runner claude-1", "remove runner claude-2", "remove runner claude-3"}
			if !reflect.DeepEqual(steps(p), want) || p.Repo != "github.com/o/r" {
				t.Fatalf("plan = %v, %+v", steps(p), p)
			}
			for _, st := range p.Changes() {
				name := hostName(st.Target)
				if st.Current != "local, GitHub not read (needs admin of github.com/o/r)" ||
					st.Detail != name+", local only: waits for a running Job to end; the GitHub registration stays" {
					t.Errorf("step %s = %q / %q", st.Target, st.Current, st.Detail)
				}
			}
			names := hostName("claude-1") + ", " + hostName("claude-2") + ", " + hostName("claude-3")
			wantNotes := []string{"The GitHub registration of " + names + " " + collaboratorNote}
			if !reflect.DeepEqual(p.Notes, wantNotes) {
				t.Errorf("notes = %q", p.Notes)
			}
			// Each slot waits for a running Job and is removed from the host
			// only; nothing is read, changed or deleted on GitHub.
			wantEvents := []string{
				"stop claude-1", "purge claude-1",
				"stop claude-2", "purge claude-2",
				"stop claude-3", "purge claude-3",
			}
			if !reflect.DeepEqual(f.events, wantEvents) {
				t.Errorf("events = %q\nwant %q", f.events, wantEvents)
			}
			if len(gh.regs) != 5 || len(gh.lists) != 0 {
				t.Errorf("GitHub touched: regs %d, lists %v", len(gh.regs), gh.lists)
			}
			m := resultJSON(t, r)
			if !reflect.DeepEqual(m["removed"], []any{"claude-1", "claude-2", "claude-3"}) || m["remaining"] != float64(0) {
				t.Errorf("result = %v", m)
			}
			if !reflect.DeepEqual(m["routes"], map[string]any{"github.com/o/r": "request"}) {
				t.Errorf("routes = %v", m["routes"])
			}
			if !reflect.DeepEqual(m["notes"], []any{wantNotes[0]}) {
				t.Errorf("result notes = %v", m["notes"])
			}
			for _, it := range m["items"].([]any) {
				it := it.(map[string]any)
				if it["action"] != actRemoveRunner {
					continue
				}
				if it["confirmed"] != true || it["state"] != "absent from the host; GitHub registration not read (needs admin of github.com/o/r)" {
					t.Errorf("item = %v", it)
				}
			}
		})
	}
}

func TestRunnerRemoveCollaboratorCount(t *testing.T) {
	f, _ := collaboratorHost(t, repoPerms{Push: true})
	// A slot a crashed add left incomplete is stale and goes before the
	// local ones.
	f.slots["claude-4"] = &fakeSlot{}
	o := RunnerRemove(RunnerRemoveParams{Provider: "claude", Count: 2})
	p, err := o.Plan(context.Background())
	want := []string{"remove runner claude-4", "remove runner claude-1"}
	if err != nil || !reflect.DeepEqual(steps(p), want) {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	if c := p.Changes()[0].Current; c != "stale, GitHub not read (needs admin of github.com/o/r)" {
		t.Errorf("stale = %q", c)
	}
	if len(p.Notes) != 1 || !strings.HasPrefix(p.Notes[0], "The GitHub registration of "+hostName("claude-4")+", "+hostName("claude-1")+" "+collaboratorNote) {
		t.Errorf("notes = %q", p.Notes)
	}
	if err := o.Apply(context.Background(), p); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"stop claude-4", "purge claude-4", "stop claude-1", "purge claude-1"}; !reflect.DeepEqual(f.events, want) {
		t.Errorf("events = %q", f.events)
	}
	if _, err := o.ReadBack(context.Background(), p); err != nil {
		t.Errorf("ReadBack: %v", err)
	}
}

func TestRunnerRemoveCollaboratorPermissionError(t *testing.T) {
	// Any other failure to read the permission fails as the runner list
	// did; nothing is removed.
	f, gh := collaboratorHost(t, repoPerms{Push: true})
	gh.permsErr = Errorf(CodeGitHubAPIFailed, "read the permissions on github.com/o/r: HTTP 502: Bad Gateway")
	_, _, err := Run(context.Background(), RunnerRemove(RunnerRemoveParams{Provider: "claude"}), &yes{})
	if Code(err) != CodeGitHubAPIFailed || len(f.events) != 0 {
		t.Errorf("err = %v, events %v", err, f.events)
	}
}
