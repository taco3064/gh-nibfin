package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type fakeOp struct {
	plan     Plan
	planErr  error
	applyErr error
	calls    []string
}

func (f *fakeOp) Plan(context.Context) (Plan, error) {
	f.calls = append(f.calls, "plan")
	return f.plan, f.planErr
}

func (f *fakeOp) Apply(context.Context, Plan) error {
	f.calls = append(f.calls, "apply")
	return f.applyErr
}

func (f *fakeOp) ReadBack(context.Context, Plan) (Result, error) {
	f.calls = append(f.calls, "readback")
	return Result{"ok": true}, nil
}

type fakeConfirmer struct {
	answer bool
	err    error
	asked  int
}

func (c *fakeConfirmer) Confirm(Plan) (bool, error) {
	c.asked++
	return c.answer, c.err
}

func TestRun(t *testing.T) {
	refusal := Errorf(CodeConfirmationRequired, "needs confirmation")
	tests := []struct {
		name      string
		plan      Plan
		confirmer fakeConfirmer
		wantCalls []string
		wantAsked int
		wantCode  string
	}{
		{"read-only never confirms or applies", Plan{ReadOnly: true, Confirm: true}, fakeConfirmer{}, []string{"plan", "readback"}, 0, ""},
		{"no confirmation when not needed", Plan{}, fakeConfirmer{}, []string{"plan", "apply", "readback"}, 0, ""},
		{"confirmed applies then reads back", Plan{Confirm: true}, fakeConfirmer{answer: true}, []string{"plan", "apply", "readback"}, 1, ""},
		{"refusal does not apply", Plan{Confirm: true}, fakeConfirmer{}, []string{"plan"}, 1, CodeCancelled},
		{"confirmer error does not apply", Plan{Confirm: true}, fakeConfirmer{err: refusal}, []string{"plan"}, 1, CodeConfirmationRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &fakeOp{plan: tt.plan}
			c := tt.confirmer
			_, r, err := Run(context.Background(), o, &c)
			if !reflect.DeepEqual(o.calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", o.calls, tt.wantCalls)
			}
			if c.asked != tt.wantAsked {
				t.Errorf("asked = %d, want %d", c.asked, tt.wantAsked)
			}
			if got := Code(err); got != tt.wantCode {
				t.Errorf("code = %q, want %q (err %v)", got, tt.wantCode, err)
			}
			if tt.wantCode == "" && r["ok"] != true {
				t.Errorf("result = %v, want the read-back result", r)
			}
		})
	}
}

func TestRunStopsOnPlanAndApplyErrors(t *testing.T) {
	o := &fakeOp{planErr: NotImplemented("x")}
	if _, _, err := Run(context.Background(), o, &fakeConfirmer{}); Code(err) != CodeNotImplemented {
		t.Fatalf("plan error code = %q", Code(err))
	}
	if !reflect.DeepEqual(o.calls, []string{"plan"}) {
		t.Errorf("calls after plan error = %v", o.calls)
	}
	o = &fakeOp{applyErr: errors.New("boom")}
	if _, _, err := Run(context.Background(), o, &fakeConfirmer{}); err == nil {
		t.Fatal("apply error was lost")
	}
	if !reflect.DeepEqual(o.calls, []string{"plan", "apply"}) {
		t.Errorf("calls after apply error = %v", o.calls)
	}
}

func TestReadBack(t *testing.T) {
	o := &fakeOp{}
	_, r, err := ReadBack(context.Background(), o)
	if err != nil || r["ok"] != true {
		t.Fatalf("ReadBack = %v, %v", r, err)
	}
	if !reflect.DeepEqual(o.calls, []string{"plan", "readback"}) {
		t.Errorf("calls = %v", o.calls)
	}
}

func TestErrorKeepsInnermostCode(t *testing.T) {
	inner := NotImplemented("status")
	err := Wrap(CodeRepoUnresolved, fmt.Errorf("context: %w", Wrap(CodeInvalidRepo, inner)))
	if got := Code(err); got != CodeNotImplemented {
		t.Fatalf("Code = %q, want %q", got, CodeNotImplemented)
	}
	if !errors.Is(err, inner) {
		t.Error("the original cause is not in the chain")
	}
	b, jerr := json.Marshal(AsError(err))
	if jerr != nil {
		t.Fatal(jerr)
	}
	var got map[string]string
	if jerr := json.Unmarshal(b, &got); jerr != nil {
		t.Fatal(jerr)
	}
	if got["code"] != CodeNotImplemented || !strings.Contains(got["message"], "status is not implemented") {
		t.Errorf("JSON = %s", b)
	}
	if Code(errors.New("plain")) != "" || Code(nil) != "" {
		t.Error("an uncoded error has a code")
	}
}

func TestNotImplemented(t *testing.T) {
	if err := NotImplemented("dashboard"); Code(err) != CodeNotImplemented || err.Error() != "dashboard is not implemented" {
		t.Errorf("NotImplemented = %v", err)
	}
}
