package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type diagnosisFunc func(context.Context, DiagnosisSelection,
	snapshot.DiagnosisAttempt, bool) *snapshot.DiagnosisProjection

func (f diagnosisFunc) Diagnose(ctx context.Context, s DiagnosisSelection,
	a snapshot.DiagnosisAttempt, diagnose bool) *snapshot.DiagnosisProjection {
	return f(ctx, s, a, diagnose)
}

func diagnosisService(t *testing.T, d Diagnoser) (*Service, diagnosisRequest) {
	t.Helper()
	s := launch(t, Config{Diagnoser: d})
	identity := snapshot.Identity{Repository: snapshot.Repository{
		ID: 1, Name: "example/project", URL: "https://github.com/example/project"},
		ID: 101, Number: 1, URL: "https://github.com/example/project/issues/1"}
	input := diagnosisRequest{Selection: DiagnosisSelection{Identity: identity,
		NodeID: "I_one", Subject: snapshot.Subject{ID: 7, Login: "reader"}},
		Attempt: snapshot.DiagnosisAttempt{Stage: "deliver",
			ID: "11111111-1111-1111-1111-111111111111"}}
	diagnose := true
	input.Diagnose = &diagnose
	issue := selectionIssueWithQualified(input.Selection)
	value := overviewValue()
	value.ObservedAt, value.ExpiresAt = time.Now(), time.Now().Add(time.Minute)
	value.Issues.Items = []snapshot.OverviewIssue{{Identity: identity, NodeID: "I_one",
		Evidence: []snapshot.LocalWork{{Issue: &issue,
			Diagnosis: &snapshot.DiagnosisProjection{Status: "reported",
				Reports: []snapshot.BlockerReport{{Binding: snapshot.ReportBinding{
					AdmissionID: input.Attempt.ID, Stage: input.Attempt.Stage}}}}}}}}
	done := make(chan struct{})
	close(done)
	s.mu.Lock()
	s.overview = &overviewState{current: value, done: done}
	s.mu.Unlock()
	return s, input
}

func diagnosisPost(t *testing.T, s *Service, path, body, token string) (int, string) {
	t.Helper()
	r, _ := http.NewRequest("POST", s.URL()+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set(SessionHeader, token)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(data)
}

func TestDiagnosisServiceFreshBindingAndStrictBoundary(t *testing.T) {
	var calls atomic.Int32
	d := diagnosisFunc(func(_ context.Context, s DiagnosisSelection,
		a snapshot.DiagnosisAttempt, diagnose bool) *snapshot.DiagnosisProjection {
		calls.Add(1)
		return &snapshot.DiagnosisProjection{Status: "no-capacity",
			Code: "DIAGNOSIS_CAPACITY_BUSY"}
	})
	s, input := diagnosisService(t, d)
	token := strings.Split(proof(s), ":")[0]
	body, _ := json.Marshal(input)
	for _, path := range []string{"/api/diagnosis?diagnose=true", "/api/%64iagnosis"} {
		if code, _ := diagnosisPost(t, s, path, string(body), token); code == 200 {
			t.Fatal("noncanonical route accepted")
		}
	}
	if code, _ := diagnosisPost(t, s, "/api/diagnosis", string(body), ""); code != 401 {
		t.Fatal(code)
	}
	bad := strings.TrimSuffix(string(body), "}") + `,"command":"shell"}`
	if code, _ := diagnosisPost(t, s, "/api/diagnosis", bad, token); code != 400 {
		t.Fatal(code)
	}
	bad = strings.TrimSuffix(string(body), "}") + `,"diagnose":true}`
	if code, _ := diagnosisPost(t, s, "/api/diagnosis", bad, token); code != 400 {
		t.Fatal("duplicate accepted", code)
	}
	for _, mode := range []string{"repository", "database", "node", "subject", "attempt"} {
		changed := input
		switch mode {
		case "repository":
			changed.Selection.Identity.Repository.ID++
		case "database":
			changed.Selection.Identity.ID++
		case "node":
			changed.Selection.NodeID = "I_other"
		case "subject":
			changed.Selection.Subject.ID++
		case "attempt":
			changed.Attempt.ID = "22222222-2222-2222-2222-222222222222"
		}
		data, _ := json.Marshal(changed)
		code, _ := diagnosisPost(t, s, "/api/diagnosis", string(data), token)
		if code != 409 {
			t.Fatal(mode, code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid selection reached Core")
	}
	code, result := diagnosisPost(t, s, "/api/diagnosis", string(body), token)
	if code != 200 || !strings.Contains(result, "no-capacity") {
		t.Fatal(code, result)
	}
	s.mu.Lock()
	s.overview.current.ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if code, _ := diagnosisPost(t, s, "/api/diagnosis", string(body), token); code != 409 {
		t.Fatal("stale selection accepted", code)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestDiagnosisServiceLifecycleAndReadSeparation(t *testing.T) {
	entered, release := make(chan bool, 1), make(chan struct{})
	d := diagnosisFunc(func(ctx context.Context, s DiagnosisSelection,
		a snapshot.DiagnosisAttempt, diagnose bool) *snapshot.DiagnosisProjection {
		entered <- diagnose
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &snapshot.DiagnosisProjection{Status: "unresolved-effect",
			Code: "DIAGNOSIS_OUTCOME_UNKNOWN"}
	})
	s, input := diagnosisService(t, d)
	readOnly := false
	input.Diagnose = &readOnly
	body, _ := json.Marshal(input)
	token := strings.Split(proof(s), ":")[0]
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		diagnosisPost(t, s, "/api/diagnosis", string(body), token)
	}()
	if <-entered {
		t.Fatal("read acquired diagnosis")
	}
	if code, _ := diagnosisPost(t, s, "/api/diagnosis", string(body), token); code != 409 {
		t.Fatal("active request not tracked", code)
	}
	// Native detail collection remains independent of diagnostic capacity.
	if code, _ := request(t, s, token); code != 200 {
		t.Fatal("diagnosis occupied repository collection slot", code)
	}
	close(release)
	<-finished
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosisServiceSanitizesUnresolvedReportContent(t *testing.T) {
	d := diagnosisFunc(func(_ context.Context, s DiagnosisSelection,
		a snapshot.DiagnosisAttempt, _ bool) *snapshot.DiagnosisProjection {
		r := snapshot.DiagnosisReport{Status: "unresolved-effect", Attempt: a,
			InvocationID: a.ID, Issue: selectionIssue(s),
			Hypotheses:    []string{"/private/hypothesis"},
			SourceReports: []string{"/private/report"}}
		r.Issue.ID = "I_other"
		return &snapshot.DiagnosisProjection{Status: "reported", Report: &r,
			Diagnoses: &snapshot.DiagnosisHistory{Status: "observed",
				Results: []snapshot.DiagnosisReport{r}}}
	})
	s, input := diagnosisService(t, d)
	body, _ := json.Marshal(input)
	token := strings.Split(proof(s), ":")[0]
	code, result := diagnosisPost(t, s, "/api/diagnosis", string(body), token)
	if code != 200 || strings.Contains(result, "private") ||
		strings.Contains(result, "I_other") {
		t.Fatal("unvalidated content reached browser", code, result)
	}
	var out snapshot.DiagnosisProjection
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "reported" || out.Report == nil || out.Diagnoses == nil ||
		len(out.Diagnoses.Results) != 1 ||
		out.Report.Status != "unresolved-effect" || out.Report.Attempt != input.Attempt ||
		out.Diagnoses.Results[0].Status != "unresolved-effect" {
		t.Fatal("unresolved observation lost", result)
	}
}
