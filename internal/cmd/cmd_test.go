package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/upgrade"
)

func execute(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// fakeRepo replaces the resolver so no test reads git remotes or gh config.
func fakeRepo(t *testing.T) *[]string {
	t.Helper()
	seen := &[]string{}
	old := repoResolver
	repoResolver = func(v string) (string, error) {
		*seen = append(*seen, v)
		if v == "bad" {
			return "", op.Errorf(op.CodeInvalidRepo, "invalid repository %q", v)
		}
		if v == "" {
			return "github.com/cur/repo", nil
		}
		return "github.com/" + v, nil
	}
	t.Cleanup(func() { repoResolver = old })
	return seen
}

func TestEveryCommandIsNotImplemented(t *testing.T) {
	fakeRepo(t)
	for _, args := range [][]string{
		{"runner", "add", "claude"},
		{"runner", "add", "codex", "--count", "2", "--yes"},
		{"runner", "remove", "claude", "--delete-login"},
		{"init"},
		{"init", "-R", "o/r", "--yes"},
		{"status"},
		{"status", "-R", "o/r"},
		{"pause", "1", "2"},
		{"resume", "3"},
		{"dashboard", "--port", "8080", "--no-browser"},
	} {
		code, stdout, stderr := execute(t, args...)
		if code == 0 || !strings.HasPrefix(stderr, "not_implemented: ") || stdout != "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestStatusJSONError(t *testing.T) {
	fakeRepo(t)
	code, stdout, stderr := execute(t, "status", "--json")
	if code == 0 || !strings.HasPrefix(stderr, "not_implemented: ") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var e map[string]string
	if err := json.Unmarshal([]byte(stdout), &e); err != nil {
		t.Fatalf("stdout is not JSON: %q", stdout)
	}
	if e["code"] != op.CodeNotImplemented || e["message"] != "status is not implemented" {
		t.Errorf("JSON error = %v", e)
	}
}

func TestBadInvocationsFail(t *testing.T) {
	fakeRepo(t)
	for _, args := range [][]string{
		{"nope"},
		{"runner"},
		{"runner", "bogus"},
		{"runner", "bogus", "claude"},
		{"runner", "add"},
		{"runner", "add", "gemini"},
		{"runner", "add", "claude", "codex"},
		{"runner", "add", "claude", "--count", "0"},
		{"runner", "add", "claude", "--count", "x"},
		{"runner", "remove"},
		{"login", "gemini"},
		{"login", "claude", "codex"},
		{"init", "extra"},
		{"pause"},
		{"resume"},
		{"dashboard", "extra"},
	} {
		code, _, stderr := execute(t, args...)
		if code == 0 || strings.HasPrefix(stderr, "not_implemented") || !strings.Contains(stderr, ": ") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	code, _, stderr := execute(t, "status", "-R", "bad")
	if code == 0 || !strings.HasPrefix(stderr, "invalid_repo: ") {
		t.Errorf("bad -R: exit %d, stderr %q", code, stderr)
	}
}

func TestHelp(t *testing.T) {
	code, stdout, _ := execute(t, "--help")
	if code != 0 || !strings.Contains(stdout, "gh nibfin") {
		t.Fatalf("exit %d, help %q", code, stdout)
	}
	for _, c := range []string{"runner", "login", "init", "upgrade", "status", "pause", "resume", "dashboard"} {
		if !strings.Contains(stdout, c) {
			t.Errorf("help lists no %s", c)
		}
	}
}

func TestFlagPlacement(t *testing.T) {
	var j bool
	root := newRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, &j)
	want := map[string][]string{
		"runner add":    {"count", "yes"},
		"runner remove": {"count", "delete-login", "yes"},
		"login":         {},
		"init":          {"repo", "yes"},
		"upgrade":       {"repo", "yes"},
		"status":        {"json", "repo"},
		"pause":         {},
		"resume":        {},
		"dashboard":     {"no-browser", "port"},
	}
	for path, flags := range want {
		c, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got := []string{}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" {
				got = append(got, f.Name)
			}
		})
		if !reflect.DeepEqual(got, flags) {
			t.Errorf("%s flags = %v, want %v", path, got, flags)
		}
		if r := c.Flags().Lookup("repo"); r != nil && r.Shorthand != "R" {
			t.Errorf("%s: --repo shorthand = %q", path, r.Shorthand)
		}
	}
}

// recordingOp stops at Plan with its own code, so a test sees the handler
// reached the entry point it replaced.
type recordingOp struct{}

func (recordingOp) Plan(context.Context) (op.Plan, error)                { return op.Plan{}, errStop }
func (recordingOp) Apply(context.Context, op.Plan) error                 { return nil }
func (recordingOp) ReadBack(context.Context, op.Plan) (op.Result, error) { return nil, nil }

var errStop = op.Errorf("stop", "stop")

func TestHandlersPassArguments(t *testing.T) {
	seenRepo := fakeRepo(t)
	old := ops
	t.Cleanup(func() { ops = old })
	var got []any
	rec := func(p any) op.Operation { got = append(got, p); return recordingOp{} }
	ops.RunnerAdd = func(p op.RunnerAddParams) op.Operation { return rec(p) }
	ops.RunnerRemove = func(p op.RunnerRemoveParams) op.Operation { return rec(p) }
	ops.Login = func(p op.LoginParams) op.Operation {
		// Not on a terminal, the handler hands the login an interactor.
		if p.Terminal != nil || p.Interact == nil {
			t.Errorf("login params = %+v", p)
		}
		return rec(op.LoginParams{Provider: p.Provider})
	}
	ops.Init = func(p op.RepoParams) op.Operation { return rec("init:" + p.Repo) }
	ops.Upgrade = func(p op.RepoParams) op.Operation { return rec("upgrade:" + p.Repo) }
	ops.Status = func(p op.RepoParams) op.Operation { return rec("status:" + p.Repo) }
	ops.Pause = func(p op.IssuesParams) op.Operation { return rec("pause:" + strings.Join(p.Issues, ",")) }
	ops.Resume = func(p op.IssuesParams) op.Operation { return rec("resume:" + strings.Join(p.Issues, ",")) }

	for _, args := range [][]string{
		{"runner", "add", "codex", "--count", "3"},
		{"runner", "remove", "claude", "--count", "2", "--delete-login"},
		{"login", "claude"},
		{"login"},
		{"init", "-R", "o/r"},
		{"upgrade"},
		{"status", "--repo", "x/y"},
		{"pause", "4", "#5"},
		{"resume", "6"},
	} {
		if code, _, stderr := execute(t, args...); code == 0 || !strings.HasPrefix(stderr, "stop: ") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
	want := []any{
		op.RunnerAddParams{Provider: "codex", Count: 3},
		op.RunnerRemoveParams{Provider: "claude", Count: 2, DeleteLogin: true},
		op.LoginParams{Provider: "claude"},
		op.LoginParams{},
		"init:github.com/o/r",
		"upgrade:github.com/cur/repo",
		"status:github.com/x/y",
		"pause:4,#5",
		"resume:6",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("params = %#v\nwant %#v", got, want)
	}
	if !reflect.DeepEqual(*seenRepo, []string{"o/r", "", "x/y"}) {
		t.Errorf("resolver saw %q", *seenRepo)
	}
}

func TestResolveRepoParse(t *testing.T) {
	got, err := resolveRepo("example.com/owner/name")
	if err != nil || got != "example.com/owner/name" {
		t.Fatalf("resolveRepo = %q, %v", got, err)
	}
	for _, v := range []string{"a/b/c/d", "noslash"} {
		if _, err := resolveRepo(v); op.Code(err) != op.CodeInvalidRepo {
			t.Errorf("%q: code %q, err %v", v, op.Code(err), err)
		}
	}
}

func TestConfirmer(t *testing.T) {
	p := op.Plan{Operation: "init", Confirm: true}
	ok, err := confirmer{yes: true}.Confirm(p)
	if !ok || err != nil {
		t.Errorf("--yes: %v, %v", ok, err)
	}
	ok, err = confirmer{stdin: strings.NewReader("y\n"), stderr: &bytes.Buffer{}}.Confirm(p)
	if ok || op.Code(err) != op.CodeConfirmationRequired {
		t.Errorf("non-terminal: %v, %v", ok, err)
	}
}

func TestTerminalNeedsTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if terminal(r, w, w) != nil || terminal(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}) != nil {
		t.Error("a pipe or buffer counts as a terminal")
	}
}

func TestInteractor(t *testing.T) {
	var out bytes.Buffer
	i := &interactor{in: bufio.NewReader(strings.NewReader("abc-123\n")), out: &out}
	i.Step(op.LoginStep{Kind: op.StepOpenURL, URL: "https://x/y"})
	i.Step(op.LoginStep{Kind: op.StepDeviceCode, Code: "ABCD-EFGH"})
	i.Step(op.LoginStep{Kind: op.StepCodeRequested})
	i.Step(op.LoginStep{Kind: op.StepSucceeded, Message: "done"})
	if want := "open https://x/y\nenter code ABCD-EFGH\npaste the code shown in your browser: "; out.String() != want {
		t.Errorf("steps printed %q", out.String())
	}
	code, err := i.Code(context.Background())
	if code != "abc-123" || err != nil {
		t.Errorf("Code = %q, %v", code, err)
	}
	if _, err := i.Code(context.Background()); op.Code(err) != op.CodeCancelled {
		t.Errorf("EOF: %v", err)
	}
	i = &interactor{in: bufio.NewReader(strings.NewReader("last")), out: &out}
	if code, err := i.Code(context.Background()); code != "last" || err != nil {
		t.Errorf("unterminated: %q, %v", code, err)
	}
}

func TestUpgradeRunsTheUpgradeOperation(t *testing.T) {
	if reflect.ValueOf(ops.Upgrade).Pointer() != reflect.ValueOf(upgrade.New).Pointer() {
		t.Error("upgrade does not call upgrade.New")
	}
}
