package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type diagnosisAPI struct{ changed string }

func (a diagnosisAPI) Call(method, path string, input, output any) error {
	if method != "GET" || input != nil {
		panic("unexpected mutation")
	}
	var data string
	switch path {
	case "user":
		data = `{"id":7,"login":"reader","type":"User"}`
		if a.changed == "subject" {
			data = `{"id":8,"login":"other","type":"User"}`
		}
	case "repos/example/project":
		data = `{"id":1,"full_name":"example/project","permissions":{"pull":true}}`
		if a.changed == "repository" {
			data = `{"id":2,"full_name":"example/project","permissions":{"pull":true}}`
		}
	case "repos/example/project/issues/1":
		data = `{"id":101,"node_id":"I_one","number":1,"state":"open",
		"url":"https://api.github.com/repos/example/project/issues/1",
		"html_url":"https://github.com/example/project/issues/1",
		"assignees":[{"id":7,"login":"reader"}]}`
		if a.changed == "node" {
			data = strings.Replace(data, "I_one", "I_other", 1)
		}
	default:
		panic(path)
	}
	return json.Unmarshal([]byte(data), output)
}

type diagnosisProcess struct {
	t        *testing.T
	mode     string
	calls    int
	request  bool
	response []byte
}

func (p *diagnosisProcess) Run(string, []string, []byte) ([]byte, error) {
	p.t.Fatal("non-context process used")
	return nil, nil
}

func (p *diagnosisProcess) RunContext(ctx context.Context, name string,
	args []string, input []byte) ([]byte, error) {
	p.calls++
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if name == "gh" {
		return []byte("private-test-credential"), nil
	}
	if len(args) != 2 || args[0] != "/trusted/core" {
		p.t.Fatal(args)
	}
	if args[1] == "help" {
		if p.mode == "legacy" {
			return []byte(`{"commands":["contract"]}`), nil
		}
		return []byte(`{"commands":["request-local-diagnosis"]}`), nil
	}
	if args[1] != "request-local-diagnosis" {
		p.t.Fatal("unintended operation", args)
	}
	p.request = true
	var body map[string]any
	if json.Unmarshal(input, &body) != nil || len(body) != 5 ||
		body["token"] != "private-test-credential" ||
		body["repository"] != "example/project" || body["issueNumber"] != float64(1) {
		p.t.Fatal("incorrect private transport")
	}
	if body["diagnose"] != (p.mode != "read") {
		p.t.Fatal("read and explicit diagnosis were conflated")
	}
	attempt, ok := body["attempt"].(map[string]any)
	if !ok || len(attempt) != 2 || attempt["stage"] != "deliver" ||
		attempt["id"] != "11111111-1111-1111-1111-111111111111" {
		p.t.Fatal("wrong admission attempt")
	}
	if p.response != nil {
		return p.response, nil
	}
	if p.mode == "lost" {
		return []byte("/private/diagnostic"), errors.New("private-test-credential")
	}
	status := "no-capacity"
	if p.mode == "denied" {
		status = "denied"
	}
	return []byte(`{"schemaVersion":1,"status":"` + status +
		`","code":"DIAGNOSIS_UNAVAILABLE","executionAuthority":false}`), nil
}

func TestDiagnosisAdapterPrivateTransportAndRefusal(t *testing.T) {
	for _, mode := range []string{"success", "legacy", "lost", "denied", "subject",
		"repository", "node"} {
		p := &diagnosisProcess{t: t, mode: mode}
		s := diagnosisSelection()
		d := dashboardDiagnoser{api: diagnosisAPI{mode}, process: p, subject: s.Subject,
			resolve: func() (string, string, error) {
				return "/trusted/node", "/trusted/core", nil
			}}
		attempt := snapshot.DiagnosisAttempt{Stage: "deliver",
			ID: "11111111-1111-1111-1111-111111111111"}
		out := d.Diagnose(context.Background(), s, attempt, true)
		expected := map[string]string{"success": "no-capacity", "legacy": "unavailable",
			"lost": "unresolved-effect", "denied": "denied", "subject": "denied",
			"repository": "denied", "node": "denied"}[mode]
		if out.Status != expected || p.request !=
			(mode == "success" || mode == "lost" || mode == "denied") {
			t.Fatal(mode, out, p.request)
		}
		data, _ := json.Marshal(out)
		if strings.Contains(string(data), "private") {
			t.Fatal("private output leaked", string(data))
		}
	}
}

func diagnosisSelection() dashboard.DiagnosisSelection {
	return dashboard.DiagnosisSelection{Identity: snapshot.Identity{
		Repository: snapshot.Repository{ID: 1, Name: "example/project"},
		ID:         101, Number: 1, URL: "https://github.com/example/project/issues/1"},
		NodeID: "I_one", Subject: snapshot.Subject{ID: 7, Login: "reader"}}
}

func TestDiagnosisAdapterReportedReadAndMismatchedResponse(t *testing.T) {
	s := diagnosisSelection()
	attempt := snapshot.DiagnosisAttempt{Stage: "deliver",
		ID: "11111111-1111-1111-1111-111111111111"}
	for _, mode := range []string{"read", "reported", "wrong-subject", "malformed"} {
		p := &diagnosisProcess{t: t, mode: mode}
		r := snapshot.DiagnosisReport{Producer: "diagnosis-agent",
			Confidence: "attributed", Explanation: "Evidence is incomplete.",
			InvocationID: attempt.ID, EvidenceID: strings.Repeat("a", 64),
			SubjectID: s.Subject.ID, Issue: snapshot.LocalIssue{
				AdoptedRoute: snapshot.AdoptedRoute{
					Repository: "example/project", RepositoryID: 1},
				Number: 1, ID: s.NodeID}, Attempt: attempt, ObservedAt: time.Now().UTC()}
		if mode == "wrong-subject" {
			r.SubjectID++
		}
		response := snapshot.DiagnosisProjection{
			SchemaVersion: 1, Status: "reported", Report: &r}
		if mode == "read" {
			response.Report = nil
			r.Status = "reported"
			response.Diagnoses = &snapshot.DiagnosisHistory{Status: "observed",
				Results: []snapshot.DiagnosisReport{r}}
			response.Reports = []snapshot.BlockerReport{{
				Reference: "report:" + strings.Repeat("b", 64),
				Binding: snapshot.ReportBinding{Issue: r.Issue, SubjectID: r.SubjectID,
					AdmissionID: attempt.ID, Stage: attempt.Stage},
				Producer: "agent", Confidence: "attributed", ObservedAt: r.ObservedAt,
				Summary: "Agent reported a blocker."}}
		}
		p.response, _ = json.Marshal(response)
		if mode == "malformed" {
			p.response = []byte("private invalid process output")
		}
		d := dashboardDiagnoser{api: diagnosisAPI{}, process: p, subject: s.Subject,
			resolve: func() (string, string, error) {
				return "/trusted/node", "/trusted/core", nil
			}}
		out := d.Diagnose(context.Background(), s, attempt, mode != "read")
		if mode == "read" || mode == "reported" {
			if mode == "read" && out.Diagnoses != nil &&
				len(out.Diagnoses.Results) == 1 && len(out.Reports) == 1 {
				continue
			}
			if out.Status != "reported" || out.Report == nil ||
				out.Report.Explanation != r.Explanation {
				t.Fatal(mode, out)
			}
		} else if out.Status != "unresolved-effect" || out.Report != nil {
			t.Fatal("untrusted response implied a known effect", mode, out)
		}
	}
}
