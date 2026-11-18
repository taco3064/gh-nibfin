package snapshot

import "time"

// Observation retains producer provenance. Value is independently nullable.
type Observation[T any] struct {
	Value      T          `json:"value"`
	Source     string     `json:"source"`
	ObservedAt time.Time  `json:"observedAt"`
	SourceTime *time.Time `json:"sourceTime"`
	Freshness  string     `json:"freshness"`
	Code       string     `json:"code"`
}

type Subject struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type AdoptedRoute struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repositoryId"`
}

type LocalJob struct {
	AdoptedRoute
	RunID     int64      `json:"runId"`
	Attempt   int        `json:"attempt"`
	JobID     int64      `json:"jobId"`
	StartedAt *time.Time `json:"startedAt"`
	URL       string     `json:"url"`
}

type LocalIssue struct {
	AdoptedRoute
	Number    int    `json:"number"`
	ID        string `json:"id"` // Core uses the node ID, not the database ID.
	Qualified string `json:"qualified"`
}

type LocalWork struct {
	Issue            *LocalIssue          `json:"issue"`
	IssueObservation Observation[*bool]   `json:"issueObservation"`
	Native           *LocalJob            `json:"native"`
	Phase            Observation[*string] `json:"phase"`
	Activity         Observation[*string] `json:"activity"`
	Pause            Observation[*bool]   `json:"pause"`
	Dependencies     Observation[*int]    `json:"dependencies"`
	ExecutionBlocker Observation[*string] `json:"executionBlocker"`
	Waiting          Observation[*string] `json:"waiting"`
}

type LocalRunner struct {
	Retained     bool   `json:"retained,omitempty"`
	Key          string `json:"key"`
	HostID       string `json:"hostId"`
	Registration struct {
		AdoptedRoute
		RunnerID int64  `json:"runnerId"`
		Name     string `json:"name"`
	} `json:"registration"`
	Routes      []AdoptedRoute         `json:"routes"`
	Adoption    Observation[*bool]     `json:"adoption"`
	Online      Observation[*bool]     `json:"online"`
	Busy        Observation[*bool]     `json:"busy"`
	State       string                 `json:"state"`
	Association Observation[*LocalJob] `json:"association"`
}

type ObservationInterval struct {
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

type LocalObservation struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Status        string              `json:"status"`
	Code          string              `json:"code,omitempty"`
	Subject       Subject             `json:"subject"`
	Interval      ObservationInterval `json:"interval"`
	Completeness  string              `json:"completeness"`
	Runners       []LocalRunner       `json:"runners"`
	Work          []LocalWork         `json:"work"`
	Errors        []string            `json:"errors"`
	// Supplementary installed inspection, never browser-supplied identities.
	AdoptedIDs []int64 `json:"-"`
}

type OverviewIssue struct {
	Retained  bool        `json:"retained,omitempty"`
	Key       string      `json:"key"`
	Identity  Identity    `json:"identity"`
	NodeID    string      `json:"node_id"`
	Title     string      `json:"title"`
	State     string      `json:"state"`
	Assignees []Assignee  `json:"assignees"`
	Evidence  []LocalWork `json:"evidence"`
	Observed  time.Time   `json:"observed_at"`
}

type Overview struct {
	SchemaVersion     int                    `json:"schema_version"`
	Subject           Subject                `json:"subject"`
	ObservedAt        time.Time              `json:"observed_at"`
	ExpiresAt         time.Time              `json:"expires_at"`
	Work              []LocalWork            `json:"work"`
	LocalInterval     ObservationInterval    `json:"local_interval"`
	LocalCompleteness string                 `json:"local_completeness"`
	LocalStatus       string                 `json:"local_status"`
	LocalCode         string                 `json:"local_code,omitempty"`
	Errors            []string               `json:"errors"`
	Discovery         Status                 `json:"discovery"`
	Repositories      Section[Repository]    `json:"repositories"`
	Issues            Section[OverviewIssue] `json:"issues"`
	Runners           Section[LocalRunner]   `json:"runners"`
}

// LocalObserver has no browser input and must finish within CollectionBound.
type LocalObserver interface {
	Observe() LocalObservation
}

type OverviewProvider struct {
	Provider
	Subject  Subject
	Observer LocalObserver
}
