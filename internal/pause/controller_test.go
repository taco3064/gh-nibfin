package pause

import (
	"context"
	"errors"
	"testing"
)

type providerFixture struct {
	facts     Facts
	reads     int
	edits     int
	editErr   error
	inspect   func(int, *Facts) error
	afterEdit func(*Facts)
}

func (p *providerFixture) Inspect(_ context.Context, _ string, _ Target) (Facts, error) {
	p.reads++
	if p.inspect != nil {
		if err := p.inspect(p.reads, &p.facts); err != nil {
			return Facts{}, err
		}
	}
	return p.facts, nil
}
func (p *providerFixture) Edit(_ context.Context, _ string, _ Target, a Action) error {
	p.edits++
	if p.afterEdit != nil {
		p.afterEdit(&p.facts)
	} else {
		p.facts.Paused = a == Pause
	}
	return p.editErr
}
func fixture() (*providerFixture, Request) {
	target := Target{RepositoryID: 1, IssueID: 2, Number: 3}
	return &providerFixture{facts: Facts{User: User{ID: 4, Login: "owner"},
			Target: target, Open: true, Scoped: true, Owner: true}},
		Request{Repository: "owner/repo", Action: Pause, Targets: []Target{target}}
}

func TestSingleAndBatchEligibility(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*Facts)
		allowed bool
	}{
		{"owner without assignment or runner", func(f *Facts) {}, true},
		{"organization admin", func(f *Facts) { f.Owner = false; f.Admin = true }, true},
		{"assigned triager", func(f *Facts) {
			f.Owner = false
			f.LabelPermission = true
			f.Assigned = true
		}, true},
		{"unassigned collaborator", func(f *Facts) {
			f.Owner = false
			f.LabelPermission = true
		}, false},
		{"assigned without permission", func(f *Facts) {
			f.Owner = false
			f.Assigned = true
		}, false},
		{"unknown identity", func(f *Facts) { f.User.ID = 0 }, false},
	}
	for _, batch := range []bool{false, true} {
		for _, action := range []Action{Pause, Resume} {
			for _, tc := range cases {
				t.Run(tc.name+string(action), func(t *testing.T) {
					p, request := fixture()
					request.Batch, request.Action = batch, action
					p.facts.Paused = action == Resume
					tc.change(&p.facts)
					c := Controller{p}
					selection := c.Review(context.Background(), request)
					result := c.Apply(context.Background(), selection)
					if (p.edits == 1) != tc.allowed || len(result) != 1 {
						t.Fatal(p.edits, result)
					}
					if tc.allowed && (result[0].Submission != "Requested" ||
						result[0].Verdict != "Unknown") {
						t.Fatal(result)
					}
				})
			}
		}
	}
}

func TestRevalidationAndAlreadyMatchingDoNotEdit(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Facts)
		action Action
	}{
		{"identity", func(f *Facts) { f.User.ID++ }, Pause},
		{"issue substitution", func(f *Facts) { f.Target.IssueID++ }, Pause},
		{"repository substitution", func(f *Facts) { f.Target.RepositoryID++ }, Pause},
		{"closed", func(f *Facts) { f.Open = false }, Pause},
		{"assignment removed", func(f *Facts) { f.Assigned = false }, Resume},
		{"permission removed", func(f *Facts) { f.LabelPermission = false }, Resume},
		{"already paused", func(f *Facts) { f.Paused = true }, Pause},
		{"already resumed", func(f *Facts) { f.Paused = false }, Resume},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, request := fixture()
			request.Action = tc.action
			p.facts.Owner, p.facts.LabelPermission, p.facts.Assigned = false, true, true
			p.facts.Paused = tc.action == Resume
			c := Controller{p}
			selection := c.Review(context.Background(), request)
			tc.change(&p.facts)
			result := c.Apply(context.Background(), selection)
			if p.edits != 0 || len(result) != 1 || result[0].Verdict != "Unknown" {
				t.Fatal(p.edits, result)
			}
		})
	}
	p, request := fixture()
	c := Controller{p}
	selection := c.Review(context.Background(), request)
	p.inspect = func(int, *Facts) error { return errors.New("offline secret") }
	result := c.Apply(context.Background(), selection)
	if p.edits != 0 || result[0].Submission != "Unknown" {
		t.Fatal(result)
	}
}

func TestIndependentSubmissionReadbackAndVerdict(t *testing.T) {
	for _, action := range []Action{Pause, Resume} {
		for _, tc := range []struct {
			name       string
			editErr    error
			read       string
			submission string
		}{
			{"matching", nil, "matching", "Requested"},
			{"mismatching", nil, "mismatching", "Requested"},
			{"unreadable", nil, "unreadable", "Requested"},
			{"rejected", Rejected, "mismatching", "Request failed"},
			{"ambiguous", errors.New("timeout"), "matching", "Unknown"},
		} {
			t.Run(tc.name+string(action), func(t *testing.T) {
				p, request := fixture()
				request.Action = action
				p.facts.Paused = action == Resume
				p.editErr = tc.editErr
				p.afterEdit = func(f *Facts) {
					if tc.read == "matching" {
						f.Paused = action == Pause
					}
				}
				p.inspect = func(n int, _ *Facts) error {
					if n == 3 && tc.read == "unreadable" {
						return errors.New("offline")
					}
					return nil
				}
				c := Controller{p}
				result := c.Apply(context.Background(), c.Review(context.Background(), request))[0]
				expected := "absent"
				if action == Resume {
					expected = "present"
				}
				if tc.read == "matching" {
					expected = "present"
					if action == Resume {
						expected = "absent"
					}
				}
				if tc.read == "unreadable" {
					expected = "unreadable"
				}
				if result.Submission != tc.submission || result.Label != expected ||
					result.Verdict != "Unknown" || p.edits != 1 {
					t.Fatal(result, p.edits)
				}
			})
		}
	}
}

func TestBatchScopeAndClosedPause(t *testing.T) {
	for _, batch := range []bool{false, true} {
		p, request := fixture()
		p.facts.Scoped, request.Batch = false, batch
		c := Controller{p}
		c.Apply(context.Background(), c.Review(context.Background(), request))
		if p.edits != 1 && !batch || p.edits != 0 && batch {
			t.Fatal(batch, p.edits)
		}
	}
	p, request := fixture()
	p.facts.Open = false
	c := Controller{p}
	c.Apply(context.Background(), c.Review(context.Background(), request))
	if p.edits != 0 {
		t.Fatal("closed Pause edited")
	}
}

func TestSingleIntentCannotSmuggleMultipleTargets(t *testing.T) {
	p, request := fixture()
	extra := request.Targets[0]
	extra.Number++
	extra.IssueID++
	request.Targets = append(request.Targets, extra)
	controller := Controller{Provider: p}
	selection := controller.Review(context.Background(), request)
	if len(selection.Items) != 0 || p.reads != 0 {
		t.Fatal(selection, p.reads)
	}
}
