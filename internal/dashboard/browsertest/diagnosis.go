package main

import (
	"context"
	"encoding/json"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type diagnosisFixture struct{}

func (diagnosisFixture) Diagnose(_ context.Context, _ dashboard.DiagnosisSelection,
	_ snapshot.DiagnosisAttempt, _ bool) *snapshot.DiagnosisProjection {
	return &snapshot.DiagnosisProjection{SchemaVersion: 1, Status: "no-capacity",
		Code: "DIAGNOSIS_CAPACITY_BUSY"}
}

// Fixture JSON bypasses the native observer. Apply the maintained public decoder
// with the fixture's authenticated subject, just as the real observer does.
func fixtureDiagnoses(data []byte, values []snapshot.Overview) {
	var raw struct {
		Overviews []struct {
			Issues struct {
				Items []struct {
					Evidence []struct {
						Diagnosis json.RawMessage `json:"diagnosis"`
					} `json:"evidence"`
				} `json:"items"`
			} `json:"issues"`
		} `json:"overviews"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	for i := range values {
		for j := range values[i].Issues.Items {
			row := &values[i].Issues.Items[j]
			for k := range row.Evidence {
				work := &row.Evidence[k]
				if work.Issue != nil {
					work.Diagnosis = snapshot.DecodeDiagnosis(
						raw.Overviews[i].Issues.Items[j].Evidence[k].Diagnosis,
						*work.Issue, values[i].Subject.ID)
				}
			}
		}
	}
}
