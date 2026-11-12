package cli

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/pause"
)

type nativeLabelAPI struct {
	user  map[string]any
	repo  map[string]any
	issue map[string]any
	fail  string
}

func (p *nativeLabelAPI) Call(method, path string, input, output any) error {
	if method != "GET" || input != nil {
		return errors.New("unexpected write")
	}
	if path == p.fail {
		return errors.New("secret /private/path")
	}
	var value any
	switch path {
	case "user":
		value = p.user
	case "repos/example/project":
		value = p.repo
	case "repos/example/project/issues/3":
		value = p.issue
	default:
		return errors.New("unexpected read")
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, output)
}
func nativeLabelFixture() (*nativeLabelAPI, pause.Target) {
	return &nativeLabelAPI{
		user: map[string]any{"id": 7, "login": "actor", "type": "User"},
		repo: map[string]any{"id": 1, "full_name": "example/project",
			"owner":       map[string]any{"id": 7, "login": "actor", "type": "User"},
			"permissions": map[string]bool{"pull": true, "push": false, "triage": false}},
		issue: map[string]any{"id": 2, "number": 3, "title": "Issue", "state": "open",
			"repository_url": "https://api.github.com/repos/example/project",
			"url":            "https://api.github.com/repos/example/project/issues/3",
			"labels":         []map[string]string{{"name": "scope"}, {"name": "pause"}},
			"assignees":      []map[string]any{}},
	}, pause.Target{RepositoryID: 1, IssueID: 2, Number: 3}
}

type labelProcess struct {
	name  string
	args  []string
	calls int
	err   error
}

func (p *labelProcess) Run(string, []string, []byte) ([]byte, error) {
	panic("must use bounded context")
}
func (p *labelProcess) RunContext(ctx context.Context, name string,
	args []string, body []byte) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok || body != nil {
		panic("missing bound or raw input")
	}
	p.name, p.args = name, append([]string(nil), args...)
	p.calls++
	return nil, p.err
}

func TestNativePauseFixedArgumentVectorsAndClassification(t *testing.T) {
	for _, action := range []pause.Action{pause.Pause, pause.Resume} {
		for _, tc := range []struct {
			err      error
			definite bool
		}{
			{nil, false}, {errors.New("PERMISSION_DENIED"), true},
			{errors.New("NOT_FOUND"), true}, {errors.New("PROCESS_RESULT_UNAVAILABLE"), false},
		} {
			process := &labelProcess{err: tc.err}
			_, target := nativeLabelFixture()
			ctx, cancel := context.WithTimeout(context.Background(), pause.WorkBound)
			err := (NativePause{Process: process}).Edit(ctx, "example/project", target, action)
			cancel()
			flag := "--add-label"
			if action == pause.Resume {
				flag = "--remove-label"
			}
			expected := []string{"issue", "edit", "3", "--repo", "example/project", flag, "pause"}
			if process.name != "gh" || !reflect.DeepEqual(process.args, expected) ||
				errors.Is(err, pause.Rejected) != tc.definite || (err == nil) != (tc.err == nil) {
				t.Fatal(process, err)
			}
		}
	}
	for _, name := range []string{"owner/repo;sh", "--help", "a/../b", "a/r\n--help"} {
		process := &labelProcess{}
		_, target := nativeLabelFixture()
		err := (NativePause{Process: process}).Edit(
			context.Background(), name, target, pause.Pause)
		if err == nil || process.calls != 0 {
			t.Fatal("invalid command reached process")
		}
	}
}

func TestNativeFactsEligibilityAndUnavailableEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*nativeLabelAPI)
		edits      bool
		unreadable bool
	}{
		{"owner unassigned", func(*nativeLabelAPI) {}, true, false},
		{"org admin unassigned", func(p *nativeLabelAPI) {
			p.repo["owner"] = map[string]any{"id": 9, "login": "example", "type": "Organization"}
			p.repo["permissions"] = map[string]bool{"admin": true}
		}, true, false},
		{"collaborator assigned", func(p *nativeLabelAPI) {
			p.repo["owner"] = map[string]any{"id": 9, "login": "example", "type": "User"}
			p.repo["permissions"] = map[string]bool{"triage": true}
			p.issue["assignees"] = []map[string]any{{"id": 7, "login": "actor"}}
		}, true, false},
		{"collaborator unassigned", func(p *nativeLabelAPI) {
			p.repo["owner"] = map[string]any{"id": 9, "login": "example", "type": "User"}
			p.repo["permissions"] = map[string]bool{"push": true}
		}, false, false},
		{"no permissions", func(p *nativeLabelAPI) {
			p.repo["permissions"] = map[string]bool{}
		}, false, true},
		{"missing identity", func(p *nativeLabelAPI) { delete(p.user, "id") }, false, true},
		{"missing labels", func(p *nativeLabelAPI) { delete(p.issue, "labels") }, false, true},
		{"missing assignees", func(p *nativeLabelAPI) {
			delete(p.issue, "assignees")
		}, false, true},
		{"repo substitution", func(p *nativeLabelAPI) { p.repo["id"] = 99 }, false, true},
		{"issue substitution", func(p *nativeLabelAPI) { p.issue["id"] = 99 }, false, true},
		{"cross repo", func(p *nativeLabelAPI) {
			p.issue["repository_url"] = "https://api.github.com/repos/other/project"
		}, false, true},
		{"PR substitution", func(p *nativeLabelAPI) {
			p.issue["pull_request"] = map[string]string{"url": "https://github.com/x"}
		}, false, true},
		{"provider denial", func(p *nativeLabelAPI) { p.fail = "user" }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, target := nativeLabelFixture()
			tc.change(api)
			process := &labelProcess{}
			provider := NativePause{API: api, Process: process}
			ctx, cancel := context.WithTimeout(context.Background(), pause.WorkBound)
			defer cancel()
			controller := pause.Controller{Provider: provider}
			request := pause.Request{Repository: "example/project", Action: pause.Resume,
				Targets: []pause.Target{target}}
			selection := controller.Review(ctx, request)
			if (selection.Items[0].Label == "unreadable") != tc.unreadable {
				t.Fatal(selection)
			}
			result := controller.Apply(ctx, selection)
			if (process.calls == 1) != tc.edits || result[0].Verdict != "Unknown" ||
				strings.Contains(result[0].Reason, "secret") {
				t.Fatal(process.calls, result)
			}
		})
	}
}
