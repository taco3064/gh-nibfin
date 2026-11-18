package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type dashboardDiagnoser struct {
	api     API
	process Process
	subject snapshot.Subject
	resolve func() (string, string, error)
}

func (d dashboardDiagnoser) Diagnose(ctx context.Context,
	s dashboard.DiagnosisSelection, attempt snapshot.DiagnosisAttempt,
	diagnose bool) *snapshot.DiagnosisProjection {
	unavailable := snapshot.UnavailableDiagnosis
	if d.process == nil || d.api == nil || !snapshot.ValidDiagnosisAttempt(attempt) ||
		s.Subject != d.subject || !snapshot.ValidRepository(s.Identity.Repository.Name) {
		return unavailable("DIAGNOSIS_UNAVAILABLE")
	}
	resolve := d.resolve
	if resolve == nil {
		resolve = overviewCore
	}
	node, entry, err := resolve()
	if err != nil {
		if err.Error() == "UNSUPPORTED_LOCAL_OBSERVATION_PLATFORM" {
			return unavailable("UNSUPPORTED_PLATFORM")
		}
		return unavailable("INSTALLED_CORE_UNAVAILABLE")
	}
	process, ok := d.process.(contextProcess)
	if !ok {
		return unavailable("CONTEXT_PROCESS_REQUIRED")
	}
	help, err := process.RunContext(ctx, node, []string{entry, "help"}, nil)
	var capability struct {
		Commands []string `json:"commands"`
	}
	if err != nil || len(help) > 65536 || json.Unmarshal(help, &capability) != nil {
		return unavailable("DIAGNOSIS_UNAVAILABLE")
	}
	if !overviewCommand(capability.Commands, "request-local-diagnosis") {
		return unavailable("UNSUPPORTED_CORE")
	}
	api := (NativePause{API: d.api, Process: d.process}).api(ctx)
	if !diagnosisNativeSelection(api, s) {
		return &snapshot.DiagnosisProjection{Status: "denied", Code: "SELECTION_CHANGED"}
	}
	// Both gh and Core resolve the same current subject. The private credential
	// never enters arguments, logs, responses, or durable Dashboard state.
	token, err := process.RunContext(ctx, "gh",
		[]string{"auth", "token", "--hostname", "github.com"}, nil)
	credential := strings.TrimSpace(string(token))
	if err != nil || len(credential) == 0 || len(credential) > 16384 ||
		strings.ContainsAny(credential, " \t\r\n\x00") {
		return unavailable("CREDENTIAL_UNAVAILABLE")
	}
	if !diagnosisNativeSelection(api, s) || ctx.Err() != nil {
		return &snapshot.DiagnosisProjection{Status: "denied", Code: "SELECTION_CHANGED"}
	}
	input, _ := json.Marshal(struct {
		Token       string                    `json:"token"`
		Repository  string                    `json:"repository"`
		IssueNumber int                       `json:"issueNumber"`
		Attempt     snapshot.DiagnosisAttempt `json:"attempt"`
		Diagnose    bool                      `json:"diagnose"`
	}{credential, s.Identity.Repository.Name, s.Identity.Number, attempt, diagnose})
	var data []byte
	if _, native := d.process.(NativeProcess); native {
		data, err = runNativeProcess(ctx, node,
			[]string{entry, "request-local-diagnosis"}, input, true)
	} else {
		data, err = process.RunContext(ctx, node,
			[]string{entry, "request-local-diagnosis"}, input)
	}
	if err != nil {
		return snapshot.UnresolvedDiagnosis()
	}
	if len(data) > 1024*1024 {
		if diagnose {
			return snapshot.UnresolvedDiagnosis()
		}
		return unavailable("INVALID_DIAGNOSIS_PROJECTION")
	}
	issue := snapshot.LocalIssue{AdoptedRoute: snapshot.AdoptedRoute{
		Repository: s.Identity.Repository.Name, RepositoryID: s.Identity.Repository.ID},
		Number: s.Identity.Number, ID: s.NodeID}
	var envelope struct {
		SchemaVersion int   `json:"schemaVersion"`
		Authority     *bool `json:"executionAuthority"`
	}
	result := snapshot.DecodeDiagnosis(data, issue, s.Subject.ID)
	if json.Unmarshal(data, &envelope) != nil || envelope.SchemaVersion != 1 ||
		envelope.Authority == nil || *envelope.Authority ||
		result.Report != nil && result.Report.Attempt != attempt {
		result = unavailable("INVALID_DIAGNOSIS_PROJECTION")
	}
	if diagnose && result.Code == "INVALID_DIAGNOSIS_PROJECTION" {
		return snapshot.UnresolvedDiagnosis()
	}
	return result
}

func diagnosisNativeSelection(api API, s dashboard.DiagnosisSelection) bool {
	user, err := dashboardSubject(api)
	if err != nil || user != s.Subject {
		return false
	}
	var repo struct {
		ID          int64           `json:"id"`
		Name        string          `json:"full_name"`
		Private     bool            `json:"private"`
		Permissions map[string]bool `json:"permissions"`
	}
	name := s.Identity.Repository.Name
	if api.Call("GET", "repos/"+name, nil, &repo) != nil ||
		repo.ID != s.Identity.Repository.ID || repo.Name != name || repo.Private ||
		!repo.Permissions["pull"] {
		return false
	}
	var issue struct {
		ID          int64              `json:"id"`
		NodeID      string             `json:"node_id"`
		Number      int                `json:"number"`
		URL         string             `json:"url"`
		HTMLURL     string             `json:"html_url"`
		State       string             `json:"state"`
		Assignees   []snapshot.Subject `json:"assignees"`
		PullRequest json.RawMessage    `json:"pull_request"`
	}
	path := "repos/" + name + "/issues/" + strconv.Itoa(s.Identity.Number)
	if api.Call("GET", path, nil, &issue) != nil || issue.ID != s.Identity.ID ||
		issue.NodeID != s.NodeID || issue.Number != s.Identity.Number ||
		issue.URL != "https://api.github.com/"+path ||
		issue.HTMLURL != s.Identity.URL || issue.State != "open" ||
		len(issue.PullRequest) != 0 && string(issue.PullRequest) != "null" {
		return false
	}
	for _, person := range issue.Assignees {
		if person == s.Subject {
			return true
		}
	}
	return false
}
