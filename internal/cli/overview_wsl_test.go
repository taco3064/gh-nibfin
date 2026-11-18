package cli

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

const overviewFixture = `{"schemaVersion":1,"status":"observed",
"subject":{"id":1,"login":"reader"},"completeness":"partial",
"interval":{"startedAt":"2026-11-18T00:00:00Z",
"finishedAt":"2026-11-18T00:00:01Z"},"runners":[],"work":[],"errors":[]}`

func fixtureOverviewBinding() overviewBinding {
	return overviewBinding{Schema: 1, Identity: strings.Repeat("a", 32),
		Architecture: "amd64", Entry: "/home/reader/.local/share/gh/" +
			"extensions/gh-nibfin/gh-nibfin", Stamp: "1:2:3:4:5", MemberID: 1}
}

func wslFixtureText(text string) []byte {
	units := utf16.Encode([]rune(text))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(data[2*i:], unit)
	}
	return data
}

type wslOverviewFixture struct {
	t               *testing.T
	names           []string
	stopped         bool
	wsl1            bool
	legacy          bool
	wrongMember     bool
	changed         bool
	changedArch     bool
	changedEntry    bool
	badBootstrap    bool
	bootstrapError  bool
	denied          bool
	wrongSubject    bool
	privateFailure  bool
	large           bool
	oversized       bool
	credentialCalls int
	privateCalls    int
	probeCalls      int
	privateTarget   string
	privateSubject  string
	unavailable     map[string]bool
	expectedContext context.Context
	cancel          context.CancelFunc
}

func (f *wslOverviewFixture) Run(name string, args []string,
	input []byte) ([]byte, error) {
	f.t.Helper()
	if name == "gh" {
		if !reflect.DeepEqual(args, []string{"auth", "token", "--hostname", "github.com"}) ||
			input != nil || f.probeCalls < 1 {
			f.t.Fatal("credential acquired before verification", args)
		}
		f.credentialCalls++
		return []byte("fixture-private-token\n"), nil
	}
	f.t.Fatal("unexpected process", name, args)
	return nil, errors.New("unexpected process")
}

func (f *wslOverviewFixture) DesktopReadContext(ctx context.Context, args []string,
	input []byte, limit int) ([]byte, error) {
	f.t.Helper()
	if f.expectedContext != nil && ctx != f.expectedContext {
		f.t.Fatal("Dashboard lifetime context replaced")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if args[0] == "--list" {
		if input != nil || limit != desktopOutputLimit {
			f.t.Fatal("inventory private input or bounds")
		}
		if len(args) == 2 && args[1] == "--verbose" {
			version := "2"
			if f.wsl1 {
				version = "1"
			}
			var lines []string
			for _, name := range f.names {
				lines = append(lines, name+" Running "+version)
			}
			return wslFixtureText(strings.Join(lines, "\n")), nil
		}
		if len(args) == 3 && f.stopped {
			return nil, nil
		}
		return wslFixtureText(strings.Join(f.names, "\n")), nil
	}
	if len(args) < 6 || args[0] != "--distribution" || args[2] != "--cd" ||
		args[3] != "~" || args[4] != "--exec" {
		f.t.Fatal("unsafe provider arguments", args)
	}
	command := args[5:]
	binding := fixtureOverviewBinding()
	if f.changedEntry {
		binding.Entry = "/home/reader/other"
	}
	if command[0] == "/usr/bin/python3" {
		if !reflect.DeepEqual(command[1:], []string{"-I", "-B", "-c", overviewBootstrap}) ||
			input != nil {
			f.t.Fatal("bootstrap received private input or shell")
		}
		if f.bootstrapError {
			return nil, errors.New("WSL guest read temporarily unavailable")
		}
		if f.badBootstrap {
			return []byte(`{"entry":"/tmp/untrusted"}`), nil
		}
		return json.Marshal(map[string]string{"entry": binding.Entry})
	}
	if len(command) < 3 || command[1] != overviewBridgeCommand {
		f.t.Fatal("arbitrary guest command", command)
	}
	if command[2] == "probe" {
		f.probeCalls++
		if input != nil || len(command) != 4 || command[3] != "1" ||
			command[0] != binding.Entry {
			f.t.Fatal("invalid credential-free probe", command)
		}
		if f.legacy || f.unavailable[args[1]] {
			return nil, errors.New("UNKNOWN_COMMAND")
		}
		if f.wrongMember {
			binding.MemberID = 2
		}
		if f.changed {
			binding.Identity = strings.Repeat("b", 32)
		}
		if f.changedArch {
			binding.Architecture = "arm64"
		}
		if f.cancel != nil {
			f.cancel()
		}
		return json.Marshal(binding)
	}
	if command[2] != "observe" || len(command) != 8 ||
		command[0] != binding.Entry || command[3] != "1" ||
		command[4] != "reader" || command[5] != binding.Identity ||
		command[6] != binding.Architecture || command[7] != binding.Stamp ||
		limit != overviewResultLimit || f.credentialCalls == 0 {
		f.t.Fatal("invalid private boundary", command)
	}
	var private map[string]string
	if json.Unmarshal(input, &private) != nil || len(private) != 1 ||
		private["token"] != "fixture-private-token" {
		f.t.Fatal("private input changed")
	}
	for _, arg := range args {
		if strings.Contains(arg, "private-token") {
			f.t.Fatal("token in argument")
		}
	}
	f.privateCalls++
	f.privateTarget = args[1]
	f.privateSubject = command[4]
	if f.privateFailure {
		return []byte("fixture-private-token"), errors.New("fixture-private-token")
	}
	if f.oversized {
		return make([]byte, overviewResultLimit+1), nil
	}
	if f.denied {
		return []byte(`{"schemaVersion":1,"status":"denied"}`), nil
	}
	observation := overviewFixture
	if f.large {
		observation = strings.TrimSuffix(observation, "}") +
			`,"padding":"` + strings.Repeat("x", 100000) + `"}`
	}
	if f.wrongSubject {
		observation = strings.Replace(observation, `"id":1`, `"id":2`, 1)
	}
	return json.Marshal(overviewGuestResult{Observation: json.RawMessage(observation),
		AdoptedIDs: []int64{77}})
}

func TestWSLOverviewSelectionAndConfinement(t *testing.T) {
	cases := []struct {
		name      string
		names     []string
		explicit  string
		configure func(*wslOverviewFixture)
		code      string
	}{
		{name: "sole", names: []string{"Ubuntu"}},
		{name: "large", names: []string{"Ubuntu"},
			configure: func(f *wslOverviewFixture) { f.large = true }},
		{name: "ambiguous", names: []string{"A", "B"},
			code: "SELECT_TARGET_WITH_TARGET_OPTION"},
		{name: "explicit", names: []string{"A", "B"}, explicit: "wsl:B"},
		{name: "missing", names: []string{"A"}, explicit: "wsl:B",
			code: "NO_RUNNING_WSL_TARGET"},
		{name: "empty", code: "NO_RUNNING_WSL_TARGET"},
		{name: "scan-bound",
			names: []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"},
			code:  "SELECT_TARGET_WITH_TARGET_OPTION"},
		{name: "stopped", names: []string{"A"},
			configure: func(f *wslOverviewFixture) { f.stopped = true },
			code:      "NO_RUNNING_WSL_TARGET"},
		{name: "wsl1", names: []string{"A"},
			configure: func(f *wslOverviewFixture) { f.wsl1 = true },
			code:      "NO_RUNNING_WSL_TARGET"},
		{name: "outdated", names: []string{"A"},
			configure: func(f *wslOverviewFixture) { f.legacy = true },
			code:      "GUEST_CLI_OR_ADOPTION_UNAVAILABLE"},
		{name: "wrong-member", names: []string{"A"},
			configure: func(f *wslOverviewFixture) { f.wrongMember = true },
			code:      "GUEST_CLI_OR_ADOPTION_UNAVAILABLE"},
		{name: "untrusted-entry", names: []string{"A"},
			configure: func(f *wslOverviewFixture) { f.badBootstrap = true },
			code:      "GUEST_CLI_OR_ADOPTION_UNAVAILABLE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &wslOverviewFixture{t: t, names: c.names}
			if c.configure != nil {
				c.configure(f)
			}
			o := &wslOverviewObserver{process: f, explicit: c.explicit,
				subject: snapshot.Subject{ID: 1, Login: "reader"}}
			result := o.Observe()
			if c.code != "" {
				if result.Code != c.code || f.credentialCalls != 0 || f.privateCalls != 0 {
					t.Fatal(result, f.credentialCalls, f.privateCalls)
				}
			} else if result.Status != "observed" || result.Completeness != "partial" ||
				!reflect.DeepEqual(result.AdoptedIDs, []int64{77}) || f.privateCalls != 1 {
				t.Fatal(result)
			}
			if c.explicit != "" && c.code == "" && f.privateTarget != "B" {
				t.Fatal("wrong guest received private input")
			}
		})
	}
}

func TestWSLOverviewRetriesUnavailableTargetRead(t *testing.T) {
	f := &wslOverviewFixture{t: t, names: []string{"A"}, bootstrapError: true}
	o := &wslOverviewObserver{process: f,
		subject: snapshot.Subject{ID: 1, Login: "reader"}}
	assertUnavailable := func(privateCalls int) {
		t.Helper()
		result := o.Observe()
		if result.Code != "GUEST_CLI_OR_ADOPTION_UNAVAILABLE" ||
			f.credentialCalls != privateCalls || f.privateCalls != privateCalls {
			t.Fatal(result, f.credentialCalls, f.privateCalls)
		}
	}
	assertUnavailable(0)
	f.bootstrapError = false
	if o.Observe().Status != "observed" || f.privateTarget != "A" {
		t.Fatal("initial discovery did not recover")
	}

	f.names = []string{"A", "B"}
	f.unavailable = map[string]bool{"A": true}
	assertUnavailable(1)
	f.unavailable = nil
	if o.Observe().Status != "observed" || f.privateTarget != "A" ||
		f.privateCalls != 2 {
		t.Fatal("refresh did not recover on the bound guest")
	}
}

func TestWSLOverviewTargetChangeNeverFallsBack(t *testing.T) {
	changes := []string{"identity", "architecture", "entry", "stopped", "removed"}
	for _, change := range changes {
		t.Run(change, func(t *testing.T) {
			f := &wslOverviewFixture{t: t, names: []string{"A"}}
			o := &wslOverviewObserver{process: f,
				subject: snapshot.Subject{ID: 1, Login: "reader"}}
			if o.Observe().Status != "observed" {
				t.Fatal("initial read failed")
			}
			f.names = []string{"A", "B"}
			switch change {
			case "identity":
				f.changed = true
			case "architecture":
				f.changedArch = true
			case "entry":
				f.changedEntry = true
			case "stopped":
				f.stopped = true
			case "removed":
				f.names = []string{"B"}
			}
			for i := 0; i < 2; i++ {
				if o.Observe().Code != "TARGET_CHANGED_RESELECT" {
					t.Fatal("change lost")
				}
			}
			if f.credentialCalls != 1 || f.privateCalls != 1 {
				t.Fatal("refresh sent private input after target changed")
			}
		})
	}
}

func TestWSLOverviewResultSafetyAndCancellation(t *testing.T) {
	for _, mode := range []string{"denied", "failure", "wrong-subject", "bound", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &wslOverviewFixture{t: t, names: []string{"A"}, denied: mode == "denied",
				privateFailure: mode == "failure", wrongSubject: mode == "wrong-subject",
				oversized: mode == "bound"}
			if mode == "cancel" {
				f.cancel = cancel
			}
			o := &wslOverviewObserver{ctx: ctx, process: f,
				subject: snapshot.Subject{ID: 1, Login: "reader"}}
			result := o.Observe()
			if result.Status == "observed" ||
				mode == "denied" && result.Status != "denied" {
				t.Fatal(result)
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), "private") {
				t.Fatal("private output leaked")
			}
			if mode == "cancel" && f.credentialCalls != 0 {
				t.Fatal("cancel ignored")
			}
		})
	}
}

func TestWSLOverviewScansPastEightUnavailableTargets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	names := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
	unavailable := make(map[string]bool)
	for _, name := range names[:8] {
		unavailable[name] = true
	}
	f := &wslOverviewFixture{t: t, names: names, unavailable: unavailable,
		expectedContext: ctx}
	o := &wslOverviewObserver{ctx: ctx, process: f,
		subject: snapshot.Subject{ID: 1, Login: "reader"}}
	if result := o.Observe(); result.Status != "observed" || f.privateTarget != "I" {
		t.Fatal("eligible target after eight unavailable guests was lost", result)
	}
	if f.probeCalls != len(names) {
		t.Fatal("expected one probe per target", f.probeCalls)
	}
}

func TestWSLOverviewStopsAtSecondVerifiedTarget(t *testing.T) {
	f := &wslOverviewFixture{t: t, names: []string{"A", "B", "C"}}
	o := &wslOverviewObserver{process: f,
		subject: snapshot.Subject{ID: 1, Login: "reader"}}
	if result := o.Observe(); result.Code != "SELECT_TARGET_WITH_TARGET_OPTION" ||
		f.probeCalls != 2 || f.credentialCalls != 0 {
		t.Fatal("ambiguous discovery continued or obtained credentials", result)
	}
}
