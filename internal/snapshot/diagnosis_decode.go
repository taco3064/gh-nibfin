package snapshot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var nativeReportPath = regexp.MustCompile(`^(issues|pull)/[1-9][0-9]*$`)
var admissionID = regexp.MustCompile(`^[a-f0-9-]{36}$`)
var reportID = regexp.MustCompile(`^report:[a-f0-9]{64}$`)
var evidenceID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var reportCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,95}$`)
var publicProse = regexp.MustCompile(`^[A-Za-z0-9 .,;:!?()'"\n_-]+$`)
var opaqueText = regexp.MustCompile(`[A-Za-z0-9_-]{40,}`)
var credentialText = regexp.MustCompile(
	`(?i)\b(?:token|password|secret|credential|bearer)\s*:`)

func ValidDiagnosisAttempt(a DiagnosisAttempt) bool {
	return admissionID.MatchString(a.ID) && (a.Stage == "continue" ||
		localValue(a.Stage) && a.Stage != "executing" && a.Stage != "queued" &&
			a.Stage != "dispatched" && a.Stage != "external-effect-unresolved" &&
			a.Stage != "no-admission-observed")
}

func sameReportIssue(a, b LocalIssue) bool {
	return (a.Qualified == "" || a.Qualified == fmt.Sprintf("%s#%d",
		a.Repository, a.Number)) && a.Repository == b.Repository &&
		a.RepositoryID == b.RepositoryID &&
		a.Number == b.Number && a.ID == b.ID
}

func safeProse(s string) bool {
	return len(s) <= 2048 && publicProse.MatchString(s) && !opaqueText.MatchString(s) &&
		!credentialText.MatchString(s)
}

func codes(values []string) bool {
	for _, value := range values {
		if !reportCode.MatchString(value) {
			return false
		}
	}
	return true
}

func validBlocker(r *BlockerReport, issue LocalIssue, subject int64) bool {
	if !validReportNative(r.Binding) || !validReportFacts(*r) ||
		!sameReportIssue(r.Binding.Issue, issue) || r.Binding.SubjectID != subject ||
		!ValidDiagnosisAttempt(r.Attempt()) || !reportID.MatchString(r.Reference) ||
		r.ObservedAt.IsZero() || r.ExecutionAuthority || !codes(r.Confirmed) ||
		!codes(r.Missing) || r.Code != nil && !reportCode.MatchString(*r.Code) {
		return false
	}
	if r.Producer != "agent" && r.Producer != "core-native" && r.Producer != "diagnosis" {
		return false
	}
	confidence := "observed"
	if r.Producer == "agent" {
		confidence = "attributed"
	}
	if r.Confidence != confidence || !safeProse(r.Summary) ||
		r.RootCause != nil && !safeProse(*r.RootCause) {
		return false
	}
	if r.FailureClass != nil {
		switch *r.FailureClass {
		case "none", "implementation_defect", "shape_defect", "authority_gap",
			"external_failure":
		default:
			return false
		}
	}
	for _, text := range r.Findings {
		if !safeProse(text) {
			return false
		}
	}
	for _, id := range r.NewerAttempts {
		if !admissionID.MatchString(id) {
			return false
		}
	}
	for _, ref := range r.References {
		if !safeReportReference(ref, *r) {
			return false
		}
	}
	// Retention does not establish a current cause, even in a recent collection.
	r.Current = false
	return true
}

func safeReportReference(ref string, r BlockerReport) bool {
	for _, repo := range []*string{&r.Binding.Issue.Repository,
		r.Binding.Native.Repository} {
		if repo == nil || !ValidRepository(*repo) {
			continue
		}
		path, ok := strings.CutPrefix(ref, "https://github.com/"+*repo+"/")
		if !ok {
			continue
		}
		if nativeReportPath.MatchString(path) {
			return true
		}
		n := r.Binding.Native
		if n.Repository != nil && *repo == *n.Repository && n.RunID != nil {
			run := fmt.Sprintf("actions/runs/%d", *n.RunID)
			if path == run || n.JobID != nil &&
				path == fmt.Sprintf("%s/job/%d", run, *n.JobID) {
				return true
			}
		}
	}
	return false
}

func validDiagnosis(r *DiagnosisReport, issue LocalIssue, subject int64) bool {
	if !ValidDiagnosisAttempt(r.Attempt) || !admissionID.MatchString(r.InvocationID) ||
		r.ExecutionAuthority {
		return false
	}
	if r.Status == "unresolved-effect" {
		// Core's in-flight projection has no report content or Issue identity.
		// Rebuild it so unvalidated report fields cannot reach serialization.
		*r = DiagnosisReport{Status: r.Status, Attempt: r.Attempt,
			InvocationID: r.InvocationID}
		return true
	}
	if r.Status != "" && r.Status != "reported" {
		return false
	}
	if !sameReportIssue(r.Issue, issue) || r.SubjectID != subject ||
		r.Producer != "diagnosis-agent" || r.Confidence != "attributed" ||
		r.ObservedAt.IsZero() || !evidenceID.MatchString(r.EvidenceID) ||
		!safeProse(r.Explanation) || len(r.Explanation) > 1024 ||
		len(r.Hypotheses) > 8 || len(r.Missing) > 8 || len(r.Confirmed) > 32 ||
		len(r.SourceReports) > 160 || !codes(r.Confirmed) ||
		len(r.SourceReports) != len(r.SourceTimes) {
		return false
	}
	for _, text := range append(append([]string{}, r.Hypotheses...), r.Missing...) {
		if !safeProse(text) || len(text) > 256 {
			return false
		}
	}
	for _, ref := range r.SourceReports {
		if !reportID.MatchString(ref) {
			return false
		}
	}
	return true
}

// Malformed additive diagnosis data must not erase unrelated Runner observations.
func DecodeDiagnosis(data []byte, issue LocalIssue, subject int64) *DiagnosisProjection {
	bad := UnavailableDiagnosis("INVALID_DIAGNOSIS_PROJECTION")
	var p DiagnosisProjection
	if len(data) > 1024*1024 || json.Unmarshal(data, &p) != nil || p.ExecutionAuthority ||
		p.Code != "" && !reportCode.MatchString(p.Code) {
		return bad
	}
	switch p.Status {
	case "reported":
	case "unavailable", "denied", "insufficient-evidence", "no-capacity",
		"unresolved-effect":
		return &DiagnosisProjection{SchemaVersion: 1, Status: p.Status, Code: p.Code}
	default:
		return bad
	}
	for i := range p.Reports {
		if !validBlocker(&p.Reports[i], issue, subject) {
			return bad
		}
	}
	if p.Report != nil && !validDiagnosis(p.Report, issue, subject) {
		return bad
	}
	if p.Diagnoses != nil {
		h := p.Diagnoses
		if h.Status != "observed" && h.Status != "unavailable" ||
			h.Code != "" && !reportCode.MatchString(h.Code) {
			return bad
		}
		for i := range h.Results {
			if !validDiagnosis(&h.Results[i], issue, subject) {
				return bad
			}
		}
	}
	return &p
}

func (w *LocalWork) UnmarshalJSON(data []byte) error {
	type plain LocalWork
	var value struct {
		*plain
		Diagnosis json.RawMessage `json:"diagnosis"`
	}
	value.plain = (*plain)(w)
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	w.diagnosisJSON = value.Diagnosis
	return nil
}
