package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// planLine is one line of the planning stream.
type planLine struct {
	Event  string
	Check  string
	Steps  []op.Step
	ID     string
	Plan   *op.Plan
	Result op.Result
	Error  *struct{ Code, Message string }
}

// planStream posts body to the planning stream of command and returns its
// status, content type and lines.
func (h *harness) planStream(command, body string, headers map[string]string) (int, string, []planLine, []byte) {
	h.t.Helper()
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/commands/"+command+"/plan/stream", body, headers))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "application/x-ndjson" {
		b, _ := readAll(resp)
		return resp.StatusCode, resp.Header.Get("Content-Type"), nil, b
	}
	var lines []planLine
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var l planLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			h.t.Fatalf("line %q: %v", sc.Text(), err)
		}
		lines = append(lines, l)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), lines, nil
}

var streamSteps = []op.Step{
	{ID: "a", Action: "set", Target: "a", Current: "old", Next: "new"},
	{ID: "b", Action: "keep", Target: "b", NoChange: true},
}

// reportingOp is a plan that reports its two items as it reads them.
func reportingOp(p op.Plan) *fakeOp {
	return &fakeOp{plan: p, planCtx: func(ctx context.Context) (op.Plan, error) {
		op.Checking(ctx, "first")
		op.Checked(ctx, streamSteps[0])
		op.Checking(ctx, "second")
		op.Checked(ctx, streamSteps[1])
		return p, nil
	}}
}

func TestPlanStreamStoresThePlan(t *testing.T) {
	h := start(t)
	if status, b, _ := h.do("POST", "/api/commands/upgrade/plan/stream", `{}`, nil); status != 401 || errCode(t, b) != op.CodeSessionRequired {
		t.Errorf("without session: %d %s", status, b)
	}
	h.open()
	if status, b, _ := h.do("POST", "/api/commands/upgrade/plan/stream", `{}`, map[string]string{"Origin": "http://evil.example"}); status != 403 || errCode(t, b) != op.CodeForbiddenOrigin {
		t.Errorf("wrong origin: %d %s", status, b)
	}
	want := op.Plan{Operation: "upgrade", Repo: "github.com/o/r", Steps: streamSteps, Confirm: true}
	o := reportingOp(want)
	h.next = func() *fakeOp { return o }
	code, ctype, lines, _ := h.planStream("upgrade", `{"repo":"o/r"}`, nil)
	if code != 200 || ctype != "application/x-ndjson" || len(lines) != 5 {
		t.Fatalf("stream: %d %q %+v", code, ctype, lines)
	}
	if lines[0].Event != "checking" || lines[0].Check != "first" || lines[2].Check != "second" {
		t.Errorf("checking lines %+v", lines)
	}
	if lines[1].Event != "checked" || !reflect.DeepEqual(lines[1].Steps, streamSteps[:1]) || !reflect.DeepEqual(lines[3].Steps, streamSteps[1:]) {
		t.Errorf("checked lines %+v", lines)
	}
	last := lines[4]
	if last.Event != "planned" || last.ID == "" || last.Plan == nil || !reflect.DeepEqual(*last.Plan, want) || last.Error != nil {
		t.Fatalf("last = %+v", last)
	}
	// The same body as the JSON route.
	if p := h.plan("upgrade", `{"repo":"o/r"}`); p.ID == "" || !reflect.DeepEqual(p.Plan, want) {
		t.Errorf("JSON plan = %+v", p)
	}
	// The stored plan applies as one from the JSON route.
	if status, events := h.apply(last.ID, `{"confirm":true}`); status != 200 || events[len(events)-1].Event != "result" {
		t.Errorf("apply: %d %+v", status, events)
	}
}

func TestPlanStreamReadOnlyAndErrors(t *testing.T) {
	h := start(t)
	h.open()
	p := op.Plan{Operation: "upgrade", Repo: "github.com/o/r", Steps: streamSteps[1:], ReadOnly: true}
	o := reportingOp(p)
	h.next = func() *fakeOp { return o }
	_, _, lines, _ := h.planStream("upgrade", `{"repo":"o/r"}`, nil)
	n := len(lines)
	if n != 6 || lines[n-2].Event != "checking" || lines[n-2].Check != "read back" {
		t.Fatalf("lines %+v", lines)
	}
	if last := lines[n-1]; last.Event != "result" || last.ID != "" || last.Result["operation"] != "upgrade" || !reflect.DeepEqual(*last.Plan, p) {
		t.Errorf("last = %+v", last)
	}

	// A failed read-back carries the plan and what was read.
	o.readBack = func() (op.Result, error) { return op.Result{"repo": "x"}, op.Errorf(op.CodeRepoUnreadable, "offline") }
	_, _, lines, _ = h.planStream("upgrade", `{"repo":"o/r"}`, nil)
	if last := lines[len(lines)-1]; last.Event != "error" || last.Error.Code != op.CodeRepoUnreadable || last.Plan == nil || last.Result["repo"] != "x" {
		t.Errorf("failed read back = %+v", last)
	}

	// A failed plan: the error alone, with its code.
	h.next = func() *fakeOp { return &fakeOp{err: op.Errorf(op.CodeAuthUnavailable, "no gh login")} }
	_, _, lines, _ = h.planStream("upgrade", `{"repo":"o/r"}`, nil)
	if len(lines) != 1 || lines[0].Event != "error" || lines[0].Error.Code != op.CodeAuthUnavailable || lines[0].Plan != nil {
		t.Errorf("failed plan = %+v", lines)
	}

	// A bad command, body or repository fails before the stream.
	if code, _, _, b := h.planStream("nope", `{}`, nil); code != 404 || errCode(t, b) != op.CodeNotFound {
		t.Errorf("unknown: %d %s", code, b)
	}
	if code, _, _, b := h.planStream("init", `{"repo":"bad"}`, nil); code != 422 || errCode(t, b) != op.CodeInvalidRepo {
		t.Errorf("bad repo: %d %s", code, b)
	}
	if code, _, _, b := h.planStream("init", `{"nope":1}`, nil); code != 400 || errCode(t, b) != op.CodeInvalidArgument {
		t.Errorf("bad body: %d %s", code, b)
	}
}

// TestPlanStreamCancels: closing the request ends the plan's context, and a
// plan that returns after that is not stored and nothing more is written.
func TestPlanStreamCancels(t *testing.T) {
	ended := make(chan error, 1)
	o := &fakeOp{planCtx: func(ctx context.Context) (op.Plan, error) {
		op.Checking(ctx, "slow")
		<-ctx.Done()
		ended <- ctx.Err()
		return op.Plan{Operation: "upgrade", Confirm: true}, nil
	}}
	s := newServer(Options{
		Ops:         Ops{Upgrade: func(op.RepoParams) op.Operation { return o }},
		ResolveRepo: func(v string) (string, error) { return "github.com/" + v, nil },
	}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequestWithContext(ctx, "POST", "/api/commands/upgrade/plan/stream", strings.NewReader(`{"repo":"o/r"}`))
	r.SetPathValue("command", "upgrade")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.planStream(w, r); close(done) }()
	cancel()
	select {
	case err := <-ended:
		if err == nil {
			t.Error("plan not cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the request did not cancel the plan")
	}
	<-done
	s.mu.Lock()
	n := len(s.plans)
	s.mu.Unlock()
	if n != 0 {
		t.Errorf("%d plans stored after a cancelled stream", n)
	}
	if body := w.Body.String(); strings.Count(body, "\n") != 1 || !strings.Contains(body, `"checking"`) {
		t.Errorf("body %q", body)
	}

	// The JSON route stores no plan for a request that ended either.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	o.planCtx = func(context.Context) (op.Plan, error) { return op.Plan{Operation: "upgrade", Confirm: true}, nil }
	r = httptest.NewRequestWithContext(ctx, "POST", "/api/commands/upgrade/plan", strings.NewReader(`{"repo":"o/r"}`))
	r.SetPathValue("command", "upgrade")
	s.plan(httptest.NewRecorder(), r)
	if len(s.plans) != 0 {
		t.Errorf("JSON route stored a plan for an ended request")
	}
}
