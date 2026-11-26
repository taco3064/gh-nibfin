package op

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

const inspectFixture = `[
 {"Id":"aaa","Config":{"Labels":{"io.github.taco3064.nibfin.repo":"acme/app","io.github.taco3064.nibfin.issue":"7"}},
  "State":{"Running":true,"StartedAt":"2026-11-26T11:57:00.123456789Z"},
  "Mounts":[{"Type":"bind","Source":"/slots/claude-1/_work/r/r/nibfin-workspace"},{"Type":"volume","Source":"/vol/nibfin-home-claude/_data"}]},
 {"Id":"bbb","Config":{"Labels":{"other":"x"}},"State":{"Running":true}},
 {"Id":"ccc","Config":{"Labels":{"io.github.taco3064.nibfin.repo":"acme/app"}},"State":{"Running":false}}
]`

func TestReadHostSlots(t *testing.T) {
	f := &fakeHost{psOut: "aaa\nbbb ccc\n", inspectOut: inspectFixture}
	fx := useRunnerHost(t, f)
	rep := ReadHostSlots(context.Background())
	if rep.Error != nil || rep.JobsError != nil || rep.Repo != "github.com/o/r" || len(rep.Slots) != 2 {
		t.Fatalf("rep = %+v", rep)
	}
	for _, s := range rep.Slots {
		if s.Name == "" || s.Dir == "" || s.Provider == "" {
			t.Errorf("slot = %+v", s)
		}
	}
	want := []JobContainer{{ID: "aaa", Repo: "acme/app", Issue: "7", StartedAt: "2026-11-26T11:57:00.123456789Z",
		Mounts: []string{"/slots/claude-1/_work/r/r/nibfin-workspace"}}}
	if !reflect.DeepEqual(rep.Jobs, want) {
		t.Errorf("jobs = %+v", rep.Jobs)
	}
	var podman []string
	for _, c := range f.calls {
		podman = append(podman, strings.Join(c, " "))
	}
	wantCalls := []string{
		"ps --quiet --no-trunc --filter label=io.github.taco3064.nibfin.repo",
		"container inspect aaa bbb ccc",
	}
	if !reflect.DeepEqual(podman, wantCalls) {
		t.Errorf("podman calls = %q", podman)
	}
	if fx.gh.tokens != 0 || len(*fx.hosts) != 0 || len(f.events) != 0 {
		t.Errorf("read GitHub or changed the host: %v %v", *fx.hosts, f.events)
	}
}

func TestReadHostSlotsNoContainers(t *testing.T) {
	f := &fakeHost{}
	useRunnerHost(t, f)
	rep := ReadHostSlots(context.Background())
	if rep.JobsError != nil || len(rep.Jobs) != 0 || rep.Jobs == nil || len(f.calls) != 1 {
		t.Errorf("rep = %+v, calls = %q", rep, f.calls)
	}
}

func TestReadHostSlotsPsFails(t *testing.T) {
	f := &fakeHost{psExit: 125}
	useRunnerHost(t, f)
	rep := ReadHostSlots(context.Background())
	if rep.Error != nil || len(rep.Slots) != 2 || rep.JobsError == nil || rep.JobsError.Code != CodePodmanFailed ||
		!strings.Contains(rep.JobsError.Error(), "ps failed") {
		t.Errorf("rep = %+v", rep)
	}
}

func TestReadHostSlotsStopped(t *testing.T) {
	f := &fakeHost{stopped: true}
	useRunnerHost(t, f)
	rep := ReadHostSlots(context.Background())
	if rep.Error == nil || rep.Error.Code != CodeMachineStopped || rep.JobsError == nil || f.starts != 0 || len(f.calls) != 0 {
		t.Errorf("rep = %+v, starts %d", rep, f.starts)
	}
}

func TestParseInspectInvalid(t *testing.T) {
	if _, err := parseInspect([]byte("not json")); Code(err) != CodePodmanFailed {
		t.Errorf("err = %v", err)
	}
}
