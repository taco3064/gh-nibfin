package op

import (
	"context"
	"slices"
	"testing"
)

func TestHostRunners(t *testing.T) {
	f := &fakeHost{slots: map[string]*fakeSlot{
		"codex-1":  done("h-codex-1", "https://github.com/o/app"),
		"claude-2": done("h-claude-2", "https://github.com/o/app"),
		"claude-3": {},
	}}
	useHost(t, f)
	info, err := HostRunners(context.Background(), false)
	if err != nil || info.Repo != "github.com/o/app" || !slices.Equal(info.Providers, []string{"claude", "codex"}) || info.Stopped || info.Describe != "fake" {
		t.Fatalf("info = %+v, %v", info, err)
	}

	useHost(t, &fakeHost{slots: map[string]*fakeSlot{"claude-1": {}}})
	if info, err = HostRunners(context.Background(), false); err != nil || info.Repo != "" || len(info.Providers) != 0 {
		t.Errorf("none = %+v, %v", info, err)
	}

	useHost(t, &fakeHost{slots: map[string]*fakeSlot{
		"claude-1": done("a", "https://github.com/o/a"),
		"codex-1":  done("b", "https://github.com/o/b"),
	}})
	if _, err = HostRunners(context.Background(), false); Code(err) != CodeRunnerRepoConflict {
		t.Errorf("conflict = %v", err)
	}
}

func TestHostRunnersStopped(t *testing.T) {
	f := &fakeHost{stopped: true}
	useHost(t, f)
	info, err := HostRunners(context.Background(), false)
	if err != nil || !info.Stopped || info.Repo != "" || f.starts != 0 || f.autoStarts != 0 {
		t.Fatalf("no start = %+v, %v, starts %d/%d", info, err, f.starts, f.autoStarts)
	}
	// Without an autostart entry the machine is started directly.
	if info, err = HostRunners(context.Background(), true); err != nil || info.Stopped || info.Repo != "github.com/o/r" || f.starts != 1 || f.autoStarts != 0 {
		t.Fatalf("start = %+v, %v, starts %d/%d", info, err, f.starts, f.autoStarts)
	}
	// With the entry present it starts through it and registers nothing.
	f = &fakeHost{stopped: true, vm: true, entry: current(winEntry())}
	useHost(t, f)
	if info, err = HostRunners(context.Background(), true); err != nil || f.starts != 0 || f.autoStarts != 1 || len(f.events) != 1 {
		t.Fatalf("autostart = %+v, %v, starts %d/%d, events %q", info, err, f.starts, f.autoStarts, f.events)
	}
}
