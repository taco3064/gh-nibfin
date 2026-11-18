package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// DiagnosisSelection binds all native identities and the authenticated subject.
// The adapter revalidates these against GitHub before private Core transport.
type DiagnosisSelection struct {
	Identity snapshot.Identity `json:"identity"`
	NodeID   string            `json:"node_id"`
	Subject  snapshot.Subject  `json:"subject"`
}

type Diagnoser interface {
	Diagnose(context.Context, DiagnosisSelection, snapshot.DiagnosisAttempt,
		bool) *snapshot.DiagnosisProjection
}

type diagnosisRequest struct {
	Selection DiagnosisSelection        `json:"selection"`
	Attempt   snapshot.DiagnosisAttempt `json:"attempt"`
	Diagnose  *bool                     `json:"diagnose"`
}

func (s *Service) diagnosis(w http.ResponseWriter, r *http.Request) {
	if status := s.authenticate(r); status != 0 {
		failure(w, status, "unauthorized")
		return
	}
	var input diagnosisRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mutationBodyLimit))
	tokens := json.NewDecoder(bytes.NewReader(body))
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err != nil || r.Header.Get("Content-Type") != "application/json" ||
		!uniqueJSON(tokens) || decoder.Decode(&input) != nil ||
		decoder.Decode(new(any)) != io.EOF || input.Diagnose == nil ||
		!snapshot.ValidDiagnosisAttempt(input.Attempt) {
		failure(w, 400, "invalid_request")
		return
	}
	s.mu.Lock()
	valid := s.diagnoser != nil && s.overview != nil &&
		diagnosisSelected(s.overview.current, input, s.now())
	active := s.diagnosisActive
	if valid && !active {
		s.diagnosisActive = true
	}
	s.mu.Unlock()
	if !valid {
		failure(w, 409, "selection_unavailable")
		return
	}
	if active {
		failure(w, 409, "request_pending")
		return
	}
	defer func() { s.mu.Lock(); s.diagnosisActive = false; s.mu.Unlock() }()
	// Core runs synchronously. Use the session lifecycle, not observation or
	// socket timeouts. Disconnects never resubmit or imply a definite failure.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	ctx, cancel := context.WithDeadline(s.mutationContext, s.expires)
	defer cancel()
	result := s.diagnoser.Diagnose(ctx, input.Selection, input.Attempt, *input.Diagnose)
	// Enforce the public projection again at the service boundary, including
	// fixture adapters. No raw process diagnostics enter a browser response.
	data, _ := json.Marshal(result)
	issue := selectionIssue(input.Selection)
	result = snapshot.DecodeDiagnosis(data, issue, input.Selection.Subject.ID)
	if result.Report != nil && result.Report.Attempt != input.Attempt {
		result = snapshot.UnavailableDiagnosis("INVALID_DIAGNOSIS_PROJECTION")
	}
	if *input.Diagnose && result.Code == "INVALID_DIAGNOSIS_PROJECTION" {
		result = snapshot.UnresolvedDiagnosis()
	}
	s.mutationResponse(w, result)
}

func selectionIssue(s DiagnosisSelection) snapshot.LocalIssue {
	return snapshot.LocalIssue{AdoptedRoute: snapshot.AdoptedRoute{
		Repository: s.Identity.Repository.Name, RepositoryID: s.Identity.Repository.ID},
		Number: s.Identity.Number, ID: s.NodeID}
}

func diagnosisSelected(value snapshot.Overview, input diagnosisRequest,
	now time.Time) bool {
	selection := input.Selection
	if value.Subject != selection.Subject || !now.Before(value.ExpiresAt) ||
		now.Before(value.ObservedAt) {
		return false
	}
	for _, row := range value.Issues.Items {
		if row.Retained || row.Identity != selection.Identity ||
			row.NodeID != selection.NodeID {
			continue
		}
		for _, evidence := range row.Evidence {
			if evidence.Diagnosis == nil || evidence.Issue == nil ||
				*evidence.Issue != selectionIssueWithQualified(selection) {
				continue
			}
			for _, report := range evidence.Diagnosis.Reports {
				if report.Attempt() == input.Attempt {
					return true
				}
			}
		}
	}
	return false
}

func selectionIssueWithQualified(s DiagnosisSelection) snapshot.LocalIssue {
	issue := selectionIssue(s)
	issue.Qualified = s.Identity.Repository.Name + "#" +
		strconv.Itoa(s.Identity.Number)
	return issue
}
