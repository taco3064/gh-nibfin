package snapshot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var nodeIssueID = regexp.MustCompile(`^I_[A-Za-z0-9_-]{1,100}$`)
var localCode = regexp.MustCompile(`^[A-Z][A-Z_]{0,63}$`)
var localLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

func UnavailableObservation(code string) LocalObservation {
	return LocalObservation{SchemaVersion: 1, Status: "unavailable", Code: code}
}

// DecodeLocalObservation discards unknown fields; raw process output is never served.
func DecodeLocalObservation(data []byte, subject Subject) LocalObservation {
	bad := UnavailableObservation("INVALID_OBSERVATION")
	var value LocalObservation
	if len(data) > 1024*1024 || json.Unmarshal(data, &value) != nil {
		return bad
	}
	if value.SchemaVersion != 1 {
		return UnavailableObservation("UNSUPPORTED_SCHEMA")
	}
	if value.Status == "denied" {
		return LocalObservation{SchemaVersion: 1, Status: "denied", Code: "ACCESS_DENIED"}
	}
	if value.Status != "observed" || value.Subject.ID != subject.ID ||
		!strings.EqualFold(value.Subject.Login, subject.Login) ||
		!localLogin.MatchString(subject.Login) || subject.ID <= 0 ||
		value.Interval.StartedAt.IsZero() || value.Interval.FinishedAt.IsZero() ||
		value.Interval.FinishedAt.Before(value.Interval.StartedAt) ||
		(value.Completeness != "complete" && value.Completeness != "partial") ||
		value.Runners == nil || value.Work == nil || len(value.Runners) > 128 ||
		len(value.Work) > 1024 || len(value.Errors) > 64 {
		return bad
	}
	value.Code = ""
	for _, code := range value.Errors {
		if !localCode.MatchString(code) {
			return bad
		}
	}
	for i := range value.Runners {
		r := &value.Runners[i]
		r.Retained = false
		if !validRoute(r.Registration.AdoptedRoute) || r.Registration.RunnerID <= 0 ||
			r.Registration.Name == "" || len(r.Registration.Name) > 128 ||
			len(r.Routes) > 32 ||
			!regexp.MustCompile(`^local-controller-[0-9]+$`).MatchString(r.HostID) {
			return bad
		}
		switch r.State {
		case "busy", "idle", "offline", "unknown":
		default:
			return bad
		}
		r.Key = fmt.Sprintf("%d:%d", r.Registration.RepositoryID,
			r.Registration.RunnerID)
		for _, route := range r.Routes {
			if !validRoute(route) {
				return bad
			}
		}
		if !validJob(r.Association.Value) || !validObservation(r.Association) ||
			!validObservation(r.Busy) || !validObservation(r.Online) ||
			!validObservation(r.Adoption) {
			return bad
		}
	}
	for i := range value.Work {
		w := &value.Work[i]
		if w.Issue != nil && len(w.diagnosisJSON) > 0 {
			w.Diagnosis = DecodeDiagnosis(w.diagnosisJSON, *w.Issue, subject.ID)
		}
		w.diagnosisJSON = nil
		if w.Issue != nil && (!validRoute(w.Issue.AdoptedRoute) ||
			w.Issue.Number <= 0 || !nodeIssueID.MatchString(w.Issue.ID) ||
			w.Issue.Qualified != fmt.Sprintf("%s#%d", w.Issue.Repository,
				w.Issue.Number)) {
			return bad
		}
		if !validJob(w.Native) || !validObservation(w.Phase) ||
			!validObservation(w.Activity) || !validObservation(w.Pause) ||
			!validObservation(w.Dependencies) || !validObservation(w.ExecutionBlocker) ||
			!validObservation(w.Waiting) || !validObservation(w.IssueObservation) {
			return bad
		}
		for _, field := range []*string{w.Phase.Value, w.Activity.Value,
			w.ExecutionBlocker.Value, w.Waiting.Value} {
			if field != nil && !localValue(*field) {
				return bad
			}
		}
	}
	return value
}

func localValue(value string) bool {
	switch value {
	case "shape", "validate-shape", "publish-shape", "deliver", "hooks", "accept",
		"commit-pr", "review-pr", "merge", "close", "wake", "executing", "queued",
		"dispatched", "external-effect-unresolved", "no-admission-observed":
		return true
	}
	return false
}

func validRoute(route AdoptedRoute) bool {
	return route.RepositoryID > 0 && ValidRepository(route.Repository)
}

func validJob(job *LocalJob) bool {
	if job == nil {
		return true
	}
	return validRoute(job.AdoptedRoute) && job.RunID > 0 && job.Attempt > 0 &&
		job.JobID > 0 && job.URL == fmt.Sprintf(
		"https://github.com/%s/actions/runs/%d/job/%d", job.Repository,
		job.RunID, job.JobID)
}

func validObservation[T any](o Observation[T]) bool {
	// Absent fields on unassociated work remain explicitly unknown.
	if o.Source == "" {
		data, _ := json.Marshal(o.Value)
		return o.ObservedAt.IsZero() && o.Code == "" && string(data) == "null"
	}
	switch o.Source {
	case "installed-adoption", "github-runner", "github-job+core-context",
		"github-job", "core-execution", "github-events+core-decisions",
		"github-dependencies", "core-journal", "github-issue+installed-adoption",
		"github-job+core-journal", "core-context+kernel-invocation",
		"core-wake-journal", "github+core-journal", "interval":
	default:
		return false
	}
	return !o.ObservedAt.IsZero() && localCode.MatchString(o.Code) &&
		(o.Freshness == "fresh" || o.Freshness == "stale" || o.Freshness == "unknown")
}
