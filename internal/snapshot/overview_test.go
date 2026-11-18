package snapshot

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type localFixture struct{ value LocalObservation }

func (f localFixture) Observe() LocalObservation { return f.value }

type overviewAPI struct {
	calls           []string
	deny            bool
	emptyAssigned   bool
	identityFailure string
}

func (a *overviewAPI) Call(method, path string, input, output any) error {
	a.calls = append(a.calls, path)
	if method != "GET" || input != nil {
		panic("unexpected write")
	}
	var data string
	switch {
	case path == "user":
		if a.identityFailure != "" {
			return fmt.Errorf("%s", a.identityFailure)
		}
		data = `{"id":7,"login":"reader"}`
	case path == "repos/example/project" || path == "repositories/1":
		if a.deny {
			return fmt.Errorf("PERMISSION_DENIED")
		}
		data = `{"id":1,"full_name":"example/project","permissions":{"pull":true}}`
	case strings.HasPrefix(path, "repos/example/project/issues?"):
		if a.emptyAssigned {
			return json.Unmarshal([]byte(`[]`), output)
		}
		if !strings.Contains(path, "assignee=reader") || strings.Contains(path, "labels=") {
			panic("not the assigned inventory")
		}
		data = `[{"id":101,"node_id":"I_one","number":1,"title":"No scope label",
		"state":"open","repository_url":"https://api.github.com/repos/example/project",
		"assignees":[{"id":7,"login":"reader"}],"labels":[]},
		{"id":102,"node_id":"I_two","number":2,"title":"Two jobs",
		"state":"open","repository_url":"https://api.github.com/repos/example/project",
		"assignees":[{"id":7,"login":"reader"}],"labels":[]},
		{"id":103,"number":3,"pull_request":{},
		"repository_url":"https://api.github.com/repos/example/project"}]`
	default:
		panic(path)
	}
	return json.Unmarshal([]byte(data), output)
}

func observedLocal(t *testing.T, count int) LocalObservation {
	t.Helper()
	now := time.Now().UTC()
	value := LocalObservation{SchemaVersion: 1, Status: "observed",
		Subject: Subject{7, "reader"}, Completeness: "complete",
		Runners: []LocalRunner{}, Work: []LocalWork{}, Errors: []string{}}
	value.Interval.StartedAt, value.Interval.FinishedAt = now, now
	for i := 0; i < count; i++ {
		r := LocalRunner{HostID: "local-controller-1000", State: "unknown",
			Routes: []AdoptedRoute{{"example/project", 1}}}
		r.Registration.AdoptedRoute = AdoptedRoute{"example/project", 1}
		r.Registration.RunnerID = int64(i + 1)
		r.Registration.Name = fmt.Sprintf("Runner %d", i)
		value.Runners = append(value.Runners, r)
	}
	data, _ := json.Marshal(value)
	result := DecodeLocalObservation(data, value.Subject)
	if result.Status != "observed" {
		t.Fatal(result)
	}
	return result
}

func TestOverviewAssignedInventoryAndIndependentEvidence(t *testing.T) {
	local := observedLocal(t, 4)
	issue := &LocalIssue{AdoptedRoute: AdoptedRoute{"example/project", 1},
		Number: 2, ID: "I_two", Qualified: "example/project#2"}
	local.Work = []LocalWork{{Issue: issue}, {Issue: issue}}
	api := &overviewAPI{}
	out := (OverviewProvider{Provider: Provider{API: api}, Subject: local.Subject,
		Observer: localFixture{local}}).CollectOverview()
	if !out.Issues.Status.Complete || len(out.Issues.Items) != 2 ||
		len(out.Runners.Items) != 4 || len(out.Issues.Items[0].Evidence) != 0 ||
		len(out.Issues.Items[1].Evidence) != 2 {
		t.Fatalf("inventory/evidence lost: %+v", out)
	}
	for _, path := range api.calls {
		if strings.Contains(path, "dependencies") || strings.Contains(path, "pulls") ||
			strings.Contains(path, "actions") {
			t.Fatal("full repository collector used", path)
		}
	}
	api.deny = true
	out = (OverviewProvider{Provider: Provider{API: api}, Subject: local.Subject,
		Observer: localFixture{local}}).CollectOverview()
	if out.Issues.Status.Availability != Denied || out.Issues.Status.Complete {
		t.Fatal("denied inventory reported complete", out.Issues)
	}
}

func TestOverviewDecodeCountsSubjectAndJobURL(t *testing.T) {
	for _, count := range []int{0, 1, 2, 5} {
		local := observedLocal(t, count)
		if len(local.Runners) != count {
			t.Fatal("pool size assumption")
		}
		data, _ := json.Marshal(local)
		wrongSubject := DecodeLocalObservation(data, Subject{8, "reader"})
		if wrongSubject.Status == "observed" {
			t.Fatal("subject mismatch accepted")
		}
	}
	local := observedLocal(t, 1)
	local.Runners[0].Association.Value = &LocalJob{
		AdoptedRoute: AdoptedRoute{"example/project", 1},
		RunID:        1, Attempt: 1, JobID: 2,
		URL: "https://evil.example/job"}
	data, _ := json.Marshal(local)
	if DecodeLocalObservation(data, local.Subject).Status == "observed" {
		t.Fatal("unsafe job URL accepted")
	}
}

func TestOverviewDeduplicatesRegistrationsAndKeepsUnknownAdmission(t *testing.T) {
	local := observedLocal(t, 1)
	local.Runners = append(local.Runners, local.Runners[0])
	out := (OverviewProvider{Provider: Provider{API: &overviewAPI{}},
		Subject: local.Subject, Observer: localFixture{local}}).CollectOverview()
	if len(out.Runners.Items) != 1 || len(out.Issues.Items[0].Evidence) != 0 {
		t.Fatal("duplicate registration or invented admission")
	}
}

func TestOverviewDecodeRetainsQualifiedIndependentFacts(t *testing.T) {
	value := observedLocal(t, 1)
	at := time.Now().UTC()
	truth, paused, count := true, true, 2
	phase, activity := "deliver", "executing"
	job := &LocalJob{AdoptedRoute: AdoptedRoute{"example/project", 1},
		RunID: 10, Attempt: 1, JobID: 20, StartedAt: &at,
		URL: "https://github.com/example/project/actions/runs/10/job/20"}
	value.Runners[0].State = "busy"
	value.Runners[0].Busy = Observation[*bool]{Value: &truth, Source: "github-runner",
		ObservedAt: at, Freshness: "unknown", Code: "NATIVE_BUSY"}
	value.Runners[0].Association = Observation[*LocalJob]{Value: job,
		Source: "github-job", ObservedAt: at, SourceTime: &at,
		Freshness: "fresh", Code: "VERIFIED_JOB"}
	work := LocalWork{Native: job, Issue: &LocalIssue{
		AdoptedRoute: AdoptedRoute{"example/project", 1}, Number: 1,
		ID: "I_one", Qualified: "example/project#1"},
		Phase: Observation[*string]{Value: &phase, Source: "core-context+kernel-invocation",
			ObservedAt: at, SourceTime: &at, Freshness: "fresh", Code: "VERIFIED_PHASE"},
		Activity: Observation[*string]{Value: &activity, Source: "github-job",
			ObservedAt: at, SourceTime: &at, Freshness: "fresh", Code: "NATIVE_JOB"},
		Pause: Observation[*bool]{Value: &paused, Source: "github-events+core-decisions",
			ObservedAt: at, SourceTime: &at, Freshness: "stale", Code: "PAUSE_EFFECTIVE"},
		Dependencies: Observation[*int]{Value: &count, Source: "github-dependencies",
			ObservedAt: at, SourceTime: &at, Freshness: "fresh", Code: "NATIVE_DEPENDENCIES"}}
	value.Work = []LocalWork{work}
	data, _ := json.Marshal(value)
	decoded := DecodeLocalObservation(data, value.Subject)
	if decoded.Status != "observed" || len(decoded.Work) != 1 ||
		decoded.Work[0].Native.JobID != 20 || *decoded.Work[0].Phase.Value != "deliver" ||
		decoded.Work[0].Pause.Freshness != "stale" || !*decoded.Work[0].Pause.Value {
		t.Fatal("independent evidence was lost", decoded)
	}
}

func TestOverviewIdentityReadFailureKeepsItsAvailabilityReason(t *testing.T) {
	local := observedLocal(t, 1)
	api := &overviewAPI{identityFailure: "RATE_LIMITED"}
	value := (OverviewProvider{Provider: Provider{API: api}, Subject: local.Subject,
		Observer: localFixture{local}}).CollectOverview()
	if value.Discovery.Availability != Unknown || value.Discovery.Reason != RateLimited {
		t.Fatal("unavailable identity was misreported as denial", value.Discovery)
	}
}

func TestOverviewEmptyAdoptionRequiresObservedInventory(t *testing.T) {
	for _, observed := range []bool{false, true} {
		local := observedLocal(t, 0)
		if observed {
			local.AdoptedIDs = []int64{}
		}
		out := (OverviewProvider{Provider: Provider{API: &overviewAPI{}},
			Subject: local.Subject, Observer: localFixture{local}}).CollectOverview()
		if out.Repositories.Status.Complete != observed ||
			len(out.Repositories.Items) != 0 {
			t.Fatal("unavailable discovery confused with empty", observed, out)
		}
	}
}

func TestOverviewAdoptedProjectWithoutAssignedIssues(t *testing.T) {
	local := observedLocal(t, 0)
	local.AdoptedIDs = []int64{1}
	api := &overviewAPI{emptyAssigned: true}
	out := (OverviewProvider{Provider: Provider{API: api}, Subject: local.Subject,
		Observer: localFixture{local}}).CollectOverview()
	if !out.Repositories.Status.Complete || len(out.Repositories.Items) != 1 ||
		out.Repositories.Items[0].ID != 1 || !out.Issues.Status.Complete ||
		len(out.Issues.Items) != 0 {
		t.Fatal("project inventory depended on Issue rows", out)
	}
}
