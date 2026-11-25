package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeWakeGH stands in for gh in the wake step: it logs each call and answers
// the permission read with FAKE_PERMISSION, or fails when that is "fail".
const fakeWakeGH = `#!/bin/bash
printf '%s\n' "$*" >>"$GH_LOG"
case "$*" in
  *"/permission"*)
    if [ "$FAKE_PERMISSION" = fail ]; then
      echo "HTTP 502" >&2
      exit 1
    fi
    echo "$FAKE_PERMISSION"
    ;;
esac
`

// TestIssueWakeScript runs the wake step of nibfin-issue.yml with a fake gh.
func TestIssueWakeScript(t *testing.T) {
	needBash(t, "jq")
	d, _ := parseIssue(t, params())
	script := d.Jobs["wake"].Steps[0].Run
	const dispatch = "workflow run nibfin-shared-runner.yml --repo o/exec -f stage=route -f repo=o/r"
	const permission = "api repos/o/r/collaborators/alice/permission --jq .permission"
	cases := []struct {
		name    string
		env     []string
		fail    bool
		calls   []string
		message string
	}{
		{
			name:  "review by a writer",
			env:   []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=o/r"},
			calls: []string{permission, dispatch + " -f branch=nibfin/issue-5 -f ref=nibfin/issue-5"},
		},
		{
			name:  "review by an admin",
			env:   []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=o/r", "FAKE_PERMISSION=admin"},
			calls: []string{permission, dispatch + " -f branch=nibfin/issue-5 -f ref=nibfin/issue-5"},
		},
		{
			name: "review on another branch",
			env:  []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=feature", "REVIEW_HEAD_REPO=o/r", "EXECUTION_REPO=", "GH_TOKEN="},
		},
		{
			name:    "review from a fork",
			env:     []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=fork/r", "EXECUTION_REPO=", "GH_TOKEN="},
			message: "from fork/r; not routed",
		},
		{
			name:    "review with no head repository",
			env:     []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=", "EXECUTION_REPO=", "GH_TOKEN="},
			message: "from an unknown repository; not routed",
		},
		{
			name:    "review without the execution variable",
			env:     []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=o/r", "EXECUTION_REPO="},
			fail:    true,
			message: "The variable NIBFIN_EXECUTION_REPO is not set",
		},
		{
			name:    "review by a reader",
			env:     []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=o/r", "FAKE_PERMISSION=read"},
			calls:   []string{permission},
			message: "alice has read permission; not routed",
		},
		{
			name:    "review by a triager",
			env:     []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=o/r", "FAKE_PERMISSION=triage"},
			calls:   []string{permission},
			message: "alice has triage permission; not routed",
		},
		{
			name:  "review with a failed permission read",
			env:   []string{"EVENT_NAME=pull_request_review", "REVIEW_BRANCH=nibfin/issue-5", "REVIEW_HEAD_REPO=o/r", "FAKE_PERMISSION=fail"},
			fail:  true,
			calls: []string{permission},
		},
		{
			name:  "issue event by a writer",
			env:   []string{"EVENT_NAME=issues", "ACTION=labeled", "LABEL=bug", "ISSUE=7", `ISSUE_LABELS=["scope"]`},
			calls: []string{permission, dispatch + " -f issue=7"},
		},
		{
			name:    "issue event by a reader",
			env:     []string{"EVENT_NAME=issues", "ACTION=labeled", "LABEL=bug", "ISSUE=7", `ISSUE_LABELS=["scope"]`, "FAKE_PERMISSION=read"},
			calls:   []string{permission},
			message: "alice has read permission; not routed",
		},
		{
			name:  "issue event with a failed permission read",
			env:   []string{"EVENT_NAME=issues", "ACTION=labeled", "LABEL=bug", "ISSUE=7", `ISSUE_LABELS=["scope"]`, "FAKE_PERMISSION=fail"},
			fail:  true,
			calls: []string{permission},
		},
		{
			name:    "push to another branch without the execution variable",
			env:     []string{"EVENT_NAME=push", "PUSH_REF=refs/heads/feature", "EXECUTION_REPO="},
			fail:    true,
			message: "The variable NIBFIN_EXECUTION_REPO is not set",
		},
		{
			name:    "issue event without the execution variable",
			env:     []string{"EVENT_NAME=issues", "ISSUE=7", `ISSUE_LABELS=[]`, "EXECUTION_REPO="},
			fail:    true,
			message: "The variable NIBFIN_EXECUTION_REPO is not set",
		},
	}
	for _, c := range cases {
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeWakeGH), 0o755); err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(dir, "gh.log")
		// Later entries override earlier ones, as in a step's env.
		env := append([]string{
			"PATH=" + bin + ":" + os.Getenv("PATH"), "GH_LOG=" + log,
			"GH_TOKEN=nibfin-token", "EXECUTION_REPO=o/exec", "REPO=o/r",
			"DEFAULT_BRANCH=main", "SENDER=alice", "FAKE_PERMISSION=write",
		}, c.env...)
		out, err := runScript(t, script, env)
		if (err != nil) != c.fail {
			t.Errorf("%s: err %v, want failure %v\n%s", c.name, err, c.fail, out)
		}
		if c.message != "" && !strings.Contains(out, c.message) {
			t.Errorf("%s: output %q has no %q", c.name, out, c.message)
		}
		b, _ := os.ReadFile(log)
		var calls []string
		if s := strings.TrimSpace(string(b)); s != "" {
			calls = strings.Split(s, "\n")
		}
		if strings.Join(calls, "\n") != strings.Join(c.calls, "\n") {
			t.Errorf("%s: gh calls\n%q\nwant\n%q\n%s", c.name, calls, c.calls, out)
		}
	}
}
