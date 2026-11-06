// Package snapshot projects native GitHub facts without delivery authority.
package snapshot

import "time"

type Availability string

const (
	Known   Availability = "known"
	Unknown Availability = "unknown"
	Denied  Availability = "denied"
)

type Reason string

const (
	None              Reason = "none"
	Unavailable       Reason = "unavailable"
	Permission        Reason = "permission_denied"
	NotFound          Reason = "not_found_or_concealed"
	RateLimited       Reason = "rate_limited"
	Invalid           Reason = "invalid_response"
	Limit             Reason = "collection_limit"
	Changed           Reason = "inconsistent_collection"
	Stale             Reason = "stale_observation"
	NoReader          Reason = "no_supported_reader"
	InvalidRepository Reason = "invalid_repository"
)

// Status describes collection completeness, never delivery or permission to act.
type Status struct {
	Availability Availability `json:"availability"`
	Complete     bool         `json:"complete"`
	Reason       Reason       `json:"reason"`
	ObservedAt   time.Time    `json:"observed_at"`
	Source       string       `json:"source"`
}

// Items is [] for known empty, null for unavailable, or partial on failure.
type Section[T any] struct {
	Status Status `json:"status"`
	Items  []T    `json:"items"`
}

type Repository struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Identity struct {
	Repository Repository `json:"repository"`
	ID         int64      `json:"id"`
	Number     int        `json:"number"`
	URL        string     `json:"url"`
}

type Assignee struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// Binding is reserved for independently checkable evidence, never inferred.
type Binding struct {
	RepositoryID int64  `json:"repository_id"`
	IssueID      int64  `json:"issue_id"`
	Revision     string `json:"revision"`
	RunID        int64  `json:"run_id"`
}

type Claim struct {
	Availability Availability `json:"availability"`
	Verification string       `json:"verification"`
	Reason       Reason       `json:"reason"`
	Source       string       `json:"source"`
	Binding      *Binding     `json:"binding"`
}

type Core struct {
	Completion Claim `json:"completion"`
	Pause      Claim `json:"pause"`
	Execution  Claim `json:"execution"`
}

type Issue struct {
	Identity  Identity      `json:"identity"`
	Title     string        `json:"title"`
	State     string        `json:"state"`
	Progress  []string      `json:"progress"`
	Assignees []Assignee    `json:"assignees"`
	Core      Core          `json:"core"`
	BlockedBy Section[Edge] `json:"blocked_by"`
}

// Reference retains an unresolved native endpoint without fabricating a node.
// Repository ID is zero only when repository lookup was unavailable.
type Reference struct {
	Identity   Identity `json:"identity"`
	Resolution Status   `json:"resolution"`
}

type Edge struct {
	From Identity  `json:"from"`
	To   Reference `json:"to"`
}

type PullRequest struct {
	Identity Identity       `json:"identity"`
	State    string         `json:"state"`
	MergedAt *time.Time     `json:"merged_at"`
	Head     string         `json:"head"`
	Checks   Section[Check] `json:"checks"`
}

type Run struct {
	Repository Repository `json:"repository"`
	ID         int64      `json:"id"`
	Attempt    int        `json:"attempt"`
	Head       string     `json:"head"`
	Status     string     `json:"status"`
	Conclusion *string    `json:"conclusion"`
	URL        string     `json:"url"`
	// Native pull_requests references, qualified by repository and head.
	PullRequests []Identity `json:"pull_requests"`
}

type Check struct {
	ID         int64   `json:"id"`
	Head       string  `json:"head"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
	URL        string  `json:"url"`
}

type Snapshot struct {
	SchemaVersion int                  `json:"schema_version"`
	StartedAt     time.Time            `json:"started_at"`
	FinishedAt    time.Time            `json:"finished_at"`
	ExpiresAt     time.Time            `json:"expires_at"`
	Repository    Section[Repository]  `json:"repository"`
	Issues        Section[Issue]       `json:"issues"`
	PullRequests  Section[PullRequest] `json:"pull_requests"`
	Runs          Section[Run]         `json:"runs"`
	Core          Core                 `json:"core"`
}

func unknownCore() Core {
	claim := Claim{Unknown, "unverified", NoReader, "none", nil}
	return Core{claim, claim, claim}
}

// At returns a copy with stale statuses, retaining facts and original provenance.
// Call before serving a cached value. No persistent cache is provided here.
func (s Snapshot) At(now time.Time) Snapshot {
	if !now.Before(s.StartedAt) && now.Before(s.ExpiresAt) {
		return s
	}
	s.Repository.Status = stale(s.Repository.Status)
	s.Issues.Status = stale(s.Issues.Status)
	s.PullRequests.Status = stale(s.PullRequests.Status)
	s.Runs.Status = stale(s.Runs.Status)
	s.Issues.Items = append([]Issue(nil), s.Issues.Items...)
	for i := range s.Issues.Items {
		issue := &s.Issues.Items[i]
		issue.BlockedBy.Status = stale(issue.BlockedBy.Status)
		issue.BlockedBy.Items = append([]Edge(nil), issue.BlockedBy.Items...)
		for j := range issue.BlockedBy.Items {
			ref := &issue.BlockedBy.Items[j].To
			ref.Resolution = stale(ref.Resolution)
		}
	}
	s.PullRequests.Items = append([]PullRequest(nil), s.PullRequests.Items...)
	for i := range s.PullRequests.Items {
		s.PullRequests.Items[i].Checks.Status = stale(s.PullRequests.Items[i].Checks.Status)
	}
	return s
}

func stale(s Status) Status {
	if s.Availability != Denied {
		s.Availability = Unknown
		s.Reason = Stale
	}
	s.Complete = false
	return s
}
