package snapshot

import (
	"fmt"
	"regexp"
	"time"
)

var reportCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)
var reportRunnerName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
var reportWorkflowPath = regexp.MustCompile(
	`^\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml$`)

type ReportTermination struct {
	Owner       string     `json:"owner"`
	ExecutionID string     `json:"executionId"`
	ContainerID string     `json:"containerId,omitempty"`
	ObservedAt  *time.Time `json:"observedAt,omitempty"`
	Running     *bool      `json:"running,omitempty"`
	ExitCode    *int       `json:"exitCode"`
	OOMKilled   *bool      `json:"oomKilled"`
	Code        string     `json:"code,omitempty"`
}

type ReportWorkflow struct {
	Owner           string `json:"owner"`
	Repository      string `json:"repository"`
	RepositoryID    int64  `json:"repositoryId"`
	RunID           int64  `json:"runId"`
	Attempt         int    `json:"attempt"`
	JobID           int64  `json:"jobId"`
	Number          int    `json:"number"`
	Head            string `json:"head"`
	Base            string `json:"base"`
	Conclusion      string `json:"conclusion"`
	URL             string `json:"url"`
	LogsUnavailable bool   `json:"logsUnavailable"`
}

func validReportFacts(r BlockerReport) bool {
	if t := r.Termination; t != nil {
		if t.ExecutionID != r.Binding.AdmissionID {
			return false
		}
		switch t.Owner {
		case "docker-inspect":
			if !evidenceID.MatchString(t.ContainerID) || t.ObservedAt == nil ||
				t.ObservedAt.IsZero() || t.Running == nil ||
				t.ExitCode != nil && (*t.ExitCode < -1 || *t.ExitCode > 255) {
				return false
			}
		case "vm-adapter":
			if !reportCode.MatchString(t.Code) || t.ContainerID != "" {
				return false
			}
		default:
			return false
		}
	}
	if w := r.Workflow; w != nil {
		if w.Owner != "github-workflow" || w.Repository != r.Binding.Issue.Repository ||
			w.RepositoryID != r.Binding.Issue.RepositoryID || w.RunID <= 0 ||
			w.JobID <= 0 || w.Number <= 0 || w.Attempt <= 0 ||
			!reportCommit.MatchString(w.Head) ||
			!reportCommit.MatchString(w.Base) ||
			w.URL != fmt.Sprintf("https://github.com/%s/actions/runs/%d/job/%d",
				w.Repository, w.RunID, w.JobID) {
			return false
		}
		switch w.Conclusion {
		case "success", "failure", "timed_out", "cancelled", "action_required",
			"neutral", "skipped", "stale", "startup_failure":
		default:
			return false
		}
	}
	return true
}

func validReportNative(b ReportBinding) bool {
	n := b.Native
	if n.Repository != nil && !ValidRepository(*n.Repository) ||
		n.Attempt != nil && *n.Attempt <= 0 {
		return false
	}
	for _, id := range []*int64{n.RepositoryID, n.RunID, n.JobID, n.RunnerID,
		n.WorkflowID} {
		if id != nil && *id <= 0 {
			return false
		}
	}
	for _, id := range []*string{b.ExecutionID, b.SessionID} {
		if id != nil && !admissionID.MatchString(*id) {
			return false
		}
	}
	return (n.RunnerName == nil ||
		reportRunnerName.MatchString(*n.RunnerName)) &&
		(n.SourceCommit == nil ||
			reportCommit.MatchString(*n.SourceCommit)) &&
		(n.WorkflowPath == nil || reportWorkflowPath.MatchString(*n.WorkflowPath))
}
