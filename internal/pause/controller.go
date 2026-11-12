package pause

import (
	"context"
	"errors"
)

func (c Controller) inspect(ctx context.Context, repo string, t Target) (Facts, error) {
	if c.Provider == nil || ctx.Err() != nil {
		return Facts{}, errors.New("unavailable")
	}
	f, err := c.Provider.Inspect(ctx, repo, t)
	if err != nil || ctx.Err() != nil || f.Target != t ||
		f.User.ID <= 0 || f.User.Login == "" {
		return Facts{}, errors.New("unavailable")
	}
	return f, nil
}

func (c Controller) Review(ctx context.Context, r Request) Selection {
	s := Selection{Request: r, Items: []Item{}}
	if !r.Valid() {
		return s
	}
	for _, t := range r.Targets {
		item := Item{Target: t, Label: "unreadable", Actor: "Unknown",
			Reason: "unavailable"}
		f, err := c.inspect(ctx, r.Repository, t)
		if err == nil {
			item.Title, item.Label, item.User = f.Title, label(f), f.User
			item.Reason = eligibility(f, r.Action, r.Batch)
			item.Eligible = item.Reason == ""
		}
		s.Items = append(s.Items, item)
	}
	return s
}

func (c Controller) Apply(ctx context.Context, s Selection) []Result {
	results := make([]Result, 0, len(s.Items))
	for _, item := range s.Items {
		result := Result{Target: item.Target, Submission: "Unknown", Label: "unreadable",
			Verdict: "Unknown", Reason: "unavailable"}
		if !item.Eligible {
			result.Reason = item.Reason
			if item.Reason != "unavailable" {
				result.Submission = "Request failed"
			}
			results = append(results, result)
			continue
		}
		f, err := c.inspect(ctx, s.Request.Repository, item.Target)
		if err == nil {
			result.Label = label(f)
			result.Reason = eligibility(f, s.Request.Action, s.Request.Batch)
			if f.User != item.User {
				result.Reason = "identity_changed"
			}
			if result.Reason != "" {
				result.Submission = "Request failed"
			} else if f.Paused == (s.Request.Action == Pause) {
				result.Submission, result.Reason = "Not needed", "already_matching"
			} else {
				err = c.Provider.Edit(ctx, s.Request.Repository, item.Target, s.Request.Action)
				result.Submission, result.Reason = "Requested", "submitted"
				if err != nil {
					result.Submission, result.Reason = "Unknown", "submission_ambiguous"
					if errors.Is(err, Rejected) {
						result.Submission, result.Reason = "Request failed", "submission_rejected"
					}
				}
				// Read-back is independent even after ambiguous or rejected submission.
				result.Label = "unreadable"
				observed, readErr := c.inspect(ctx, s.Request.Repository, item.Target)
				if readErr == nil {
					result.Label = label(observed)
				}
			}
		}
		results = append(results, result)
	}
	return results
}
