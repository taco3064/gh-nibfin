package templates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
)

type wf struct{ name, path, state string }

// fakeREST serves a workflow list in pages and file contents from memory.
type fakeREST struct {
	workflows []wf
	files     map[string]string
	failList  error
	failFile  map[string]error
	encoding  string
	requests  []string
}

func (f *fakeREST) DoWithContext(_ context.Context, method, p string, _ io.Reader, response any) error {
	f.requests = append(f.requests, method+" "+p)
	u, err := url.Parse(p)
	if err != nil {
		return err
	}
	var body any
	switch {
	case u.Path == "repos/o/r/actions/workflows":
		if f.failList != nil {
			return f.failList
		}
		if u.Query().Get("per_page") != "100" {
			return fmt.Errorf("per_page %q", u.Query().Get("per_page"))
		}
		var page int
		fmt.Sscan(u.Query().Get("page"), &page)
		size := 100
		lo, hi := min((page-1)*size, len(f.workflows)), min(page*size, len(f.workflows))
		items := []map[string]string{}
		for _, w := range f.workflows[lo:hi] {
			items = append(items, map[string]string{"name": w.name, "path": w.path, "state": w.state})
		}
		body = map[string]any{"total_count": len(f.workflows), "workflows": items}
	case strings.HasPrefix(u.Path, "repos/o/r/contents/"):
		name := strings.TrimPrefix(u.Path, "repos/o/r/contents/")
		if err := f.failFile[name]; err != nil {
			return err
		}
		content, ok := f.files[name]
		if !ok {
			return &api.HTTPError{StatusCode: 404, Message: "Not Found"}
		}
		enc := base64.StdEncoding.EncodeToString([]byte(content))
		// GitHub wraps the base64 content in lines.
		var wrapped []string
		for len(enc) > 60 {
			wrapped, enc = append(wrapped, enc[:60]), enc[60:]
		}
		wrapped = append(wrapped, enc)
		encoding := f.encoding
		if encoding == "" {
			encoding = "base64"
		}
		body = map[string]string{"encoding": encoding, "content": strings.Join(wrapped, "\n") + "\n"}
	default:
		return fmt.Errorf("unexpected request %s", p)
	}
	b, _ := json.Marshal(body)
	return json.Unmarshal(b, response)
}

func TestCIWorkflows(t *testing.T) {
	f := &fakeREST{
		workflows: []wf{
			{"CI", ".github/workflows/ci.yml", "active"},
			{"Image", ".github/workflows/image.yml", "active"},
			{"Lint", ".github/workflows/lint.yml", "active"},
			{"Queue", ".github/workflows/queue.yml", "active"},
			{"Target", ".github/workflows/target.yml", "active"},
			{"Nightly", ".github/workflows/nightly.yml", "active"},
			{"Manual", ".github/workflows/manual.yml", "active"},
			{"Old", ".github/workflows/old.yml", "disabled_manually"},
			{"Nibfin · Issue events", ".github/workflows/nibfin-issue.yml", "active"},
			{"Nibfin · Job", ".github/workflows/nibfin-job.yml", "active"},
			{"CodeQL", "dynamic/github-code-scanning/codeql", "active"},
		},
		files: map[string]string{
			".github/workflows/ci.yml":      "name: CI\non:\n  pull_request:\n  push:\n    branches: [main]\n",
			".github/workflows/image.yml":   "name: Image\non: push\n",
			".github/workflows/lint.yml":    "name: Lint\non: [workflow_dispatch, pull_request]\n",
			".github/workflows/queue.yml":   "on:\n  merge_group:\n    types: [checks_requested]\n",
			".github/workflows/target.yml":  "on:\n  pull_request_target:\n",
			".github/workflows/nightly.yml": "on:\n  schedule:\n    - cron: '0 0 * * *'\n",
			".github/workflows/manual.yml":  "on: workflow_dispatch\n",
		},
	}
	// Pad the list to three pages of 100; the CI/CD workflows sit on every page.
	for i := len(f.workflows); i < 250; i++ {
		f.workflows = append(f.workflows, wf{fmt.Sprint("Off ", i), fmt.Sprintf(".github/workflows/off-%d.yml", i), "disabled_inactivity"})
	}
	f.workflows[120], f.workflows[3] = f.workflows[3], f.workflows[120]
	f.workflows[249], f.workflows[4] = f.workflows[4], f.workflows[249]
	got, err := CIWorkflows(context.Background(), f, "github.com/o/r")
	if err != nil {
		t.Fatalf("CIWorkflows: %v", err)
	}
	if want := []string{"CI", "Image", "Lint", "Queue", "Target"}; !slices.Equal(slices.Sorted(slices.Values(got)), want) {
		t.Errorf("CIWorkflows = %q, want %q", got, want)
	}
	for _, r := range f.requests {
		for _, skipped := range []string{"old.yml", "nibfin-", "dynamic"} {
			if strings.Contains(r, "contents/") && strings.Contains(r, skipped) {
				t.Errorf("read an excluded workflow: %s", r)
			}
		}
	}
	pages := 0
	for _, r := range f.requests {
		if strings.Contains(r, "actions/workflows") {
			pages++
		}
	}
	if pages != 3 {
		t.Errorf("listed %d pages, want 3 (250 workflows in pages of 100)", pages)
	}
}

func TestCIWorkflowsNoneAndCore(t *testing.T) {
	// gh-nibfin today: CI plus the Core-calling caller.
	f := &fakeREST{
		workflows: []wf{
			{"CI", ".github/workflows/ci.yml", "active"},
			{"Nibfin · Issue events", ".github/workflows/nibfin-issue.yml", "active"},
		},
		files: map[string]string{".github/workflows/ci.yml": "name: CI\non:\n  pull_request:\n  push:\n    branches: [main]\n"},
	}
	got, err := CIWorkflows(context.Background(), f, "github.com/o/r")
	if err != nil || !slices.Equal(got, []string{"CI"}) {
		t.Errorf("CIWorkflows = %q, %v; want [CI]", got, err)
	}
	got, err = CIWorkflows(context.Background(), &fakeREST{}, "github.com/o/r")
	if err != nil || len(got) != 0 {
		t.Errorf("no workflows: %q, %v", got, err)
	}
}

func TestCIWorkflowsErrors(t *testing.T) {
	one := []wf{{"CI", ".github/workflows/ci.yml", "active"}}
	cases := map[string]struct {
		f    *fakeREST
		repo string
		code string
	}{
		"bad repo":   {&fakeREST{}, "o/r", op.CodeInvalidRepo},
		"list fails": {&fakeREST{failList: errors.New("HTTP 403: Forbidden")}, "github.com/o/r", op.CodeWorkflowsUnreadable},
		"file fails": {&fakeREST{workflows: one, failFile: map[string]error{".github/workflows/ci.yml": errors.New("HTTP 500")}}, "github.com/o/r", op.CodeWorkflowsUnreadable},
		"bad yaml":   {&fakeREST{workflows: one, files: map[string]string{".github/workflows/ci.yml": "on: [push\n"}}, "github.com/o/r", op.CodeWorkflowsUnreadable},
		"bad on":     {&fakeREST{workflows: one, files: map[string]string{".github/workflows/ci.yml": "on: [[push]]\n"}}, "github.com/o/r", op.CodeWorkflowsUnreadable},
		"not base64": {&fakeREST{workflows: one, files: map[string]string{".github/workflows/ci.yml": "on: push\n"}, encoding: "none"}, "github.com/o/r", op.CodeWorkflowsUnreadable},
	}
	for name, c := range cases {
		_, err := CIWorkflows(context.Background(), c.f, c.repo)
		if got := op.Code(err); got != c.code {
			t.Errorf("%s: code %q (%v), want %s", name, got, err, c.code)
		}
	}
	// The original cause stays in the chain.
	cause := errors.New("HTTP 403: Forbidden")
	_, err := CIWorkflows(context.Background(), &fakeREST{failList: cause}, "github.com/o/r")
	if !errors.Is(err, cause) {
		t.Errorf("the cause is lost: %v", err)
	}
}

func TestTriggers(t *testing.T) {
	cases := map[string][]string{
		"on: push\n":                                 {"push"},
		"on: [push, pull_request]\n":                 {"push", "pull_request"},
		"on:\n  push:\n  schedule:\n    - cron: x\n": {"push", "schedule"},
		"name: no trigger\n":                         nil,
		"true: x\non: push\n":                        {"push"},
	}
	for in, want := range cases {
		got, err := triggers([]byte(in))
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("triggers(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestReadFile(t *testing.T) {
	ctx := context.Background()
	f := &fakeREST{files: map[string]string{".github/workflows/nibfin-issue.yml": "name: x\n"}}
	data, found, err := ReadFile(ctx, f, "github.com/o/r", ".github/workflows/nibfin-issue.yml", "nibfin/upgrade")
	if err != nil || !found || string(data) != "name: x\n" {
		t.Fatalf("ReadFile = %q, %v, %v", data, found, err)
	}
	if want := "GET repos/o/r/contents/.github/workflows/nibfin-issue.yml?ref=nibfin%2Fupgrade"; f.requests[0] != want {
		t.Errorf("request %q, want %q", f.requests[0], want)
	}
	// A 404 is a missing file, not an error.
	data, found, err = ReadFile(ctx, f, "github.com/o/r", ".github/workflows/nibfin-job.yml", "")
	if err != nil || found || data != nil {
		t.Errorf("missing file = %q, %v, %v", data, found, err)
	}
	if want := "GET repos/o/r/contents/.github/workflows/nibfin-job.yml"; f.requests[1] != want {
		t.Errorf("request %q, want %q", f.requests[1], want)
	}
	// Any other failure is returned as it is.
	cause := &api.HTTPError{StatusCode: 403, Message: "Forbidden"}
	f.failFile = map[string]error{".github/workflows/nibfin-issue.yml": cause}
	if _, found, err := ReadFile(ctx, f, "github.com/o/r", ".github/workflows/nibfin-issue.yml", ""); found || !errors.Is(err, cause) {
		t.Errorf("403 = %v, %v", found, err)
	}
	if _, _, err := ReadFile(ctx, f, "o/r", "x", ""); op.Code(err) != op.CodeInvalidRepo {
		t.Errorf("bad repo: %v", err)
	}
}

func TestCIWorkflowsMissingFile(t *testing.T) {
	f := &fakeREST{workflows: []wf{{"CI", ".github/workflows/ci.yml", "active"}}}
	if _, err := CIWorkflows(context.Background(), f, "github.com/o/r"); op.Code(err) != op.CodeWorkflowsUnreadable {
		t.Errorf("missing workflow file: %v", err)
	}
}
