package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

const overviewHostFixture = `{"schemaVersion":1,"memberId":1,
"repository":"example/project","repositoryId":77,"adoptedRepositoryIds":[77],
"runners":[{"id":10,"name":"one"},{"id":11,"name":"two"}]}`

type overviewBridgeFixture struct {
	t                *testing.T
	calls            []string
	member           bool
	noCapability     bool
	wrongSubject     bool
	failedInspection bool
	expectedContext  context.Context
	cancelObserve    context.CancelFunc
}

func (p *overviewBridgeFixture) Run(name string, args []string,
	input []byte) ([]byte, error) {
	p.t.Helper()
	if name != "/trusted/node" || len(args) != 2 || args[0] != "/trusted/core" {
		p.t.Fatal("bridge accessed a login or arbitrary command", name, args)
	}
	p.calls = append(p.calls, args[1])
	switch args[1] {
	case "help":
		if input != nil {
			p.t.Fatal("credential in help")
		}
		if p.noCapability {
			return []byte(`{"commands":[]}`), nil
		}
		return []byte(`{"commands":["observe-local-work","inspect-shared-host"]}`), nil
	case "inspect-shared-host":
		if input != nil {
			p.t.Fatal("credential in inspection")
		}
		if p.failedInspection && len(p.calls) > 3 {
			return nil, errors.New("private diagnostic")
		}
		data := overviewHostFixture
		if p.member {
			data = strings.Replace(data, `"memberId":1`, `"memberId":2`, 1)
		}
		return []byte(data), nil
	case "observe-local-work":
		if string(input) != `{"token":"fixture-private-token"}` {
			p.t.Fatal("bridge did not confine private input")
		}
		data := overviewFixture
		if p.wrongSubject {
			data = strings.Replace(data, `"id":1`, `"id":2`, 1)
		}
		return []byte(data), nil
	}
	p.t.Fatal("bridge selected arbitrary Core operation")
	return nil, errors.New("unexpected operation")
}

func overviewBridgeLoader() (overviewBinding, string, string, error) {
	binding := fixtureOverviewBinding()
	binding.MemberID = 0
	return binding, "/trusted/node", "/trusted/core", nil
}

func TestOverviewBridgeProbeIsCredentialFree(t *testing.T) {
	p := &overviewBridgeFixture{t: t}
	var out bytes.Buffer
	err := executeOverviewBridge(context.Background(), p, []string{"probe", "1"},
		&out, forbiddenOverviewInput{t}, overviewBridgeLoader)
	var binding overviewBinding
	if err != nil || json.Unmarshal(out.Bytes(), &binding) != nil ||
		binding != fixtureOverviewBinding() || len(p.calls) != 2 {
		t.Fatal("invalid probe", err, out.String())
	}
}

type forbiddenOverviewInput struct{ t *testing.T }

func (f forbiddenOverviewInput) Read([]byte) (int, error) {
	f.t.Fatal("probe or refusal read private input")
	return 0, io.EOF
}

func TestOverviewBridgeObservationAndRefusals(t *testing.T) {
	b := fixtureOverviewBinding()
	base := []string{"observe", "1", "reader", b.Identity, b.Architecture, b.Stamp}
	cases := []struct {
		name      string
		args      []string
		input     string
		configure func(*overviewBridgeFixture)
		refuse    bool
	}{
		{name: "success", args: base, input: `{"token":"fixture-private-token"}`},
		{name: "extra-field", args: base, input: `{"token":"value","path":"/tmp"}`,
			refuse: true},
		{name: "invalid-token", args: base, input: `{"token":"a b"}`, refuse: true},
		{name: "oversized", args: base, input: strings.Repeat("x", observationInputLimit+1),
			refuse: true},
		{name: "extra-argument", args: append(append([]string{}, base...), "help"),
			refuse: true},
		{name: "wrong-member", args: base,
			configure: func(p *overviewBridgeFixture) { p.member = true }, refuse: true},
		{name: "old-core", args: base,
			configure: func(p *overviewBridgeFixture) { p.noCapability = true }, refuse: true},
		{name: "changed-identity", args: []string{
			"observe", "1", "reader", strings.Repeat("b", 32), b.Architecture, b.Stamp},
			refuse: true},
		{name: "changed-stamp", args: []string{
			"observe", "1", "reader", b.Identity, b.Architecture, "2:3"}, refuse: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &overviewBridgeFixture{t: t}
			if c.configure != nil {
				c.configure(p)
			}
			var out bytes.Buffer
			err := executeOverviewBridge(context.Background(), p, c.args, &out,
				strings.NewReader(c.input), overviewBridgeLoader)
			if (err != nil) != c.refuse {
				t.Fatal("wrong refusal", err)
			}
			if c.refuse && out.Len() != 0 {
				t.Fatal("refusal output leaked")
			}
			if !c.refuse {
				var result overviewGuestResult
				if json.Unmarshal(out.Bytes(), &result) != nil || result.Partial ||
					len(result.AdoptedIDs) != 1 || len(p.calls) != 4 {
					t.Fatal("incomplete bridge response", out.String(), p.calls)
				}
			}
			if strings.Contains(out.String(), "private") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestOverviewBridgePreservesPartialAndRejectsWrongSubject(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		p := &overviewBridgeFixture{t: t, failedInspection: !wrong, wrongSubject: wrong}
		b := fixtureOverviewBinding()
		var out bytes.Buffer
		err := executeOverviewBridge(context.Background(), p,
			[]string{"observe", "1", "reader", b.Identity, b.Architecture, b.Stamp}, &out,
			strings.NewReader(`{"token":"fixture-private-token"}`), overviewBridgeLoader)
		var result overviewGuestResult
		if err != nil || json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal(err)
		}
		if wrong && strings.Contains(string(result.Observation), `"status":"observed"`) {
			t.Fatal("wrong subject accepted")
		}
		if !wrong && (!result.Partial || len(result.AdoptedIDs) != 0) {
			t.Fatal("missing supplementary inventory was complete")
		}
	}
}

func (p *overviewBridgeFixture) RunContext(ctx context.Context, name string,
	args []string, input []byte) ([]byte, error) {
	p.t.Helper()
	if p.expectedContext != nil && ctx != p.expectedContext {
		p.t.Fatal("guest lifetime context replaced by an observation deadline")
	}
	if len(args) == 2 && args[1] == "observe-local-work" && p.cancelObserve != nil {
		p.cancelObserve()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return p.Run(name, args, input)
}

func TestOverviewBridgeUsesCallerLifetime(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		p := &overviewBridgeFixture{t: t, expectedContext: ctx}
		if interrupted {
			p.cancelObserve = cancel
		}
		b := fixtureOverviewBinding()
		var out bytes.Buffer
		err := executeOverviewBridge(ctx, p,
			[]string{"observe", "1", "reader", b.Identity, b.Architecture, b.Stamp},
			&out, strings.NewReader(`{"token":"fixture-private-token"}`),
			overviewBridgeLoader)
		cancel()
		if (err != nil) != interrupted || interrupted && out.Len() != 0 {
			t.Fatal("guest observation did not follow caller cancellation", err)
		}
	}
}
