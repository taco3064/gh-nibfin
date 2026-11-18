package snapshot

import "time"

// Diagnosis projections contain only Core's public consumer fields.
// They convey attribution and retained evidence, never execution authority.
type DiagnosisAttempt struct {
	ID    string `json:"id"`
	Stage string `json:"stage"`
}

type ReportNative struct {
	RunnerID     *int64  `json:"runnerId"`
	RunnerName   *string `json:"runnerName"`
	SourceCommit *string `json:"sourceCommit"`
	WorkflowID   *int64  `json:"workflowId"`
	WorkflowPath *string `json:"workflowPath"`
	Repository   *string `json:"repository"`
	RepositoryID *int64  `json:"repositoryId"`
	RunID        *int64  `json:"runId"`
	Attempt      *int    `json:"attempt"`
	JobID        *int64  `json:"jobId"`
}

type ReportBinding struct {
	ExecutionID *string      `json:"executionId"`
	SessionID   *string      `json:"sessionId"`
	Issue       LocalIssue   `json:"issue"`
	Stage       string       `json:"stage"`
	SubjectID   int64        `json:"subjectId"`
	AdmissionID string       `json:"admissionId"`
	Native      ReportNative `json:"native"`
}

type BlockerReport struct {
	Termination        *ReportTermination `json:"termination"`
	Workflow           *ReportWorkflow    `json:"workflow"`
	Reference          string             `json:"reference"`
	Binding            ReportBinding      `json:"binding"`
	Producer           string             `json:"producer"`
	Confidence         string             `json:"confidence"`
	ObservedAt         time.Time          `json:"observedAt"`
	FailureClass       *string            `json:"failureClass"`
	Summary            string             `json:"summary"`
	RootCause          *string            `json:"rootCause"`
	Findings           []string           `json:"findings"`
	Code               *string            `json:"code"`
	Confirmed          []string           `json:"confirmed"`
	Missing            []string           `json:"missing"`
	References         []string           `json:"references"`
	Current            bool               `json:"current"`
	Selected           bool               `json:"selected"`
	NewerAttempts      []string           `json:"newerAttempts"`
	ExecutionAuthority bool               `json:"executionAuthority"`
}

type DiagnosisReport struct {
	Status             string           `json:"status,omitempty"`
	Producer           string           `json:"producer"`
	Confidence         string           `json:"confidence"`
	Explanation        string           `json:"explanation"`
	Hypotheses         []string         `json:"hypotheses"`
	Missing            []string         `json:"missing"`
	InvocationID       string           `json:"invocationId"`
	EvidenceID         string           `json:"evidenceId"`
	SubjectID          int64            `json:"subjectId"`
	Issue              LocalIssue       `json:"issue"`
	Attempt            DiagnosisAttempt `json:"attempt"`
	ObservedAt         time.Time        `json:"observedAt"`
	Confirmed          []string         `json:"confirmed"`
	SourceReports      []string         `json:"sourceReports"`
	SourceTimes        []*time.Time     `json:"sourceTimes"`
	ExecutionAuthority bool             `json:"executionAuthority"`
}

type DiagnosisHistory struct {
	Status  string            `json:"status"`
	Code    string            `json:"code,omitempty"`
	Results []DiagnosisReport `json:"results"`
}

type DiagnosisProjection struct {
	SchemaVersion      int               `json:"schemaVersion,omitempty"`
	Status             string            `json:"status"`
	Code               string            `json:"code,omitempty"`
	Reports            []BlockerReport   `json:"reports"`
	Diagnoses          *DiagnosisHistory `json:"diagnoses,omitempty"`
	Report             *DiagnosisReport  `json:"report,omitempty"`
	ExecutionAuthority bool              `json:"executionAuthority"`
}

func UnavailableDiagnosis(code string) *DiagnosisProjection {
	return &DiagnosisProjection{SchemaVersion: 1, Status: "unavailable", Code: code}
}

func (r BlockerReport) Attempt() DiagnosisAttempt {
	return DiagnosisAttempt{ID: r.Binding.AdmissionID, Stage: r.Binding.Stage}
}

func UnresolvedDiagnosis() *DiagnosisProjection {
	return &DiagnosisProjection{SchemaVersion: 1, Status: "unresolved-effect",
		Code: "DIAGNOSIS_OUTCOME_UNKNOWN"}
}
