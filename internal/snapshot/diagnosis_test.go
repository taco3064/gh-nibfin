package snapshot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func blockerFixture() (LocalIssue, BlockerReport) {
	issue := LocalIssue{AdoptedRoute: AdoptedRoute{"example/project", 1},
		Number: 1, ID: "I_one", Qualified: "example/project#1"}
	r := BlockerReport{Reference: "report:" + strings.Repeat("a", 64),
		Binding: ReportBinding{Issue: issue, SubjectID: 7, Stage: "deliver",
			AdmissionID: "11111111-1111-1111-1111-111111111111"},
		Producer: "agent", Confidence: "attributed", ObservedAt: time.Now().UTC(),
		Summary: "Agent reported a blocker.", Confirmed: []string{},
		Missing:    []string{"INDEPENDENT_CAUSE_CONFIRMATION_UNAVAILABLE"},
		References: []string{"https://github.com/example/project/issues/1"}}
	return issue, r
}

func TestDiagnosisProjectionBindingAndSafeOutput(t *testing.T) {
	issue, report := blockerFixture()
	for _, mode := range []string{"valid", "repository", "node", "subject", "path",
		"prose", "authority", "confidence", "attempt", "native-path", "qualified"} {
		r := report
		switch mode {
		case "repository":
			r.Binding.Issue.RepositoryID++
		case "node":
			r.Binding.Issue.ID = "I_collision"
		case "subject":
			r.Binding.SubjectID++
		case "path":
			r.References = []string{"https://github.com/example/project/blob/main/private"}
		case "prose":
			text := "/private/secret"
			r.RootCause = &text
		case "authority":
			r.ExecutionAuthority = true
		case "confidence":
			r.Confidence = "observed"
		case "native-path":
			private := "/private/report"
			r.Binding.Native.Repository = &private
		case "qualified":
			r.Binding.Issue.Qualified = "/private/report"
		case "attempt":
			r.Binding.AdmissionID = "1"
		}
		data, _ := json.Marshal(DiagnosisProjection{Status: "reported",
			Reports: []BlockerReport{r}})
		out := DecodeDiagnosis(data, issue, 7)
		if (out.Status == "reported") != (mode == "valid") {
			t.Fatal(mode, out)
		}
		if mode == "valid" && out.Reports[0].Current {
			t.Fatal("retention became current cause")
		}
	}
}

func TestDiagnosisMalformedAdditionPreservesObservation(t *testing.T) {
	local := observedLocal(t, 1)
	issue, report := blockerFixture()
	local.Work = []LocalWork{{Issue: &issue, Diagnosis: &DiagnosisProjection{
		Status: "reported", Reports: []BlockerReport{report}}}}
	data, _ := json.Marshal(local)
	valid := DecodeLocalObservation(data, local.Subject)
	if valid.Status != "observed" || valid.Work[0].Diagnosis.Status != "reported" {
		t.Fatal(valid)
	}
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	work := raw["work"].([]any)[0].(map[string]any)
	work["diagnosis"] = map[string]any{"status": "reported", "reports": "malformed"}
	data, _ = json.Marshal(raw)
	out := DecodeLocalObservation(data, local.Subject)
	if out.Status != "observed" || len(out.Runners) != 1 ||
		out.Work[0].Diagnosis.Status != "unavailable" {
		t.Fatal("additive failure erased usable observations", out)
	}
}

func TestDiagnosisHistoryRetainsSourceTimesAndUnknownEffect(t *testing.T) {
	issue, blocker := blockerFixture()
	r := DiagnosisReport{Status: "reported", Producer: "diagnosis-agent",
		Confidence: "attributed", Explanation: "The evidence is incomplete.",
		InvocationID: blocker.Binding.AdmissionID, EvidenceID: strings.Repeat("b", 64),
		SubjectID: 7, Issue: issue, Attempt: blocker.Attempt(),
		ObservedAt: time.Now().UTC(), SourceReports: []string{blocker.Reference},
		SourceTimes: []*time.Time{&blocker.ObservedAt}}
	unknown := DiagnosisReport{Status: "unresolved-effect", Attempt: blocker.Attempt(),
		InvocationID: blocker.Binding.AdmissionID}
	p := DiagnosisProjection{Status: "reported", Diagnoses: &DiagnosisHistory{
		Status: "observed", Results: []DiagnosisReport{r, unknown}}}
	data, _ := json.Marshal(p)
	out := DecodeDiagnosis(data, issue, 7)
	if out.Status != "reported" || len(out.Diagnoses.Results) != 2 ||
		!out.Diagnoses.Results[0].SourceTimes[0].Equal(blocker.ObservedAt) {
		t.Fatal(out)
	}
}

func TestUnresolvedDiagnosisOmitsUnsupportedFields(t *testing.T) {
	issue, blocker := blockerFixture()
	allowed := DiagnosisReport{Status: "unresolved-effect", Attempt: blocker.Attempt(),
		InvocationID: blocker.Binding.AdmissionID}
	unsafe := allowed
	unsafe.Producer = "/private/producer"
	unsafe.Explanation = "/private/explanation"
	unsafe.SubjectID = 99
	unsafe.Issue = issue
	unsafe.Issue.ID = "I_other"
	unsafe.Confidence = "/private/confidence"
	unsafe.EvidenceID = "/private/evidence"
	unsafe.Hypotheses = []string{"/private/hypothesis"}
	unsafe.Missing = []string{"/private/missing"}
	unsafe.Confirmed = []string{"/private/confirmed"}
	unsafe.SourceReports = []string{"/private/report"}
	unsafe.SourceTimes = []*time.Time{&blocker.ObservedAt}
	unsafe.ObservedAt = blocker.ObservedAt
	for _, location := range []string{"report", "history"} {
		t.Run(location, func(t *testing.T) {
			p := DiagnosisProjection{Status: "reported"}
			if location == "report" {
				p.Report = &unsafe
			} else {
				p.Diagnoses = &DiagnosisHistory{Status: "observed",
					Results: []DiagnosisReport{unsafe}}
			}
			data, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			out := DecodeDiagnosis(data, issue, 7)
			if out.Status != "reported" {
				t.Fatal("unresolved observation lost", out)
			}
			got := out.Report
			if location == "history" {
				got = &out.Diagnoses.Results[0]
			}
			serialized, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := json.Marshal(allowed)
			if string(serialized) != string(want) {
				t.Fatalf("unsupported unresolved fields serialized: %s", serialized)
			}
		})
	}
}

func TestUnresolvedDiagnosisRejectsInvalidEnvelope(t *testing.T) {
	issue, blocker := blockerFixture()
	for _, mode := range []string{"attempt", "stage", "invocation", "authority"} {
		t.Run(mode, func(t *testing.T) {
			r := DiagnosisReport{Status: "unresolved-effect", Attempt: blocker.Attempt(),
				InvocationID: blocker.Binding.AdmissionID}
			switch mode {
			case "attempt":
				r.Attempt.ID = "/private/attempt"
			case "stage":
				r.Attempt.Stage = "/private/stage"
			case "invocation":
				r.InvocationID = "/private/invocation"
			case "authority":
				r.ExecutionAuthority = true
			}
			data, _ := json.Marshal(DiagnosisProjection{Status: "reported", Report: &r})
			out := DecodeDiagnosis(data, issue, 7)
			if out.Code != "INVALID_DIAGNOSIS_PROJECTION" || out.Report != nil {
				t.Fatal("invalid unresolved record accepted", out)
			}
		})
	}
}
