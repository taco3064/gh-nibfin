package op

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/repair"
)

const (
	coreID   = 99
	meID     = 7
	issueURL = "https://github.com/o/r/issues/1"
)

// fakeDecision serves one Issue's events and comments and takes comments.
type fakeDecision struct {
	events   []map[string]any
	comments []map[string]any
	clock    time.Time
	nextID   int64
	failPost error
	posts    []string
	gets     int
}

func (f *fakeDecision) User(context.Context) (ghUser, error) {
	return ghUser{ID: meID, Login: "me"}, nil
}

func (f *fakeDecision) Get(_ context.Context, path string, v any) error {
	f.gets++
	list := f.events
	switch {
	case strings.HasPrefix(path, "repos/o/r/issues/1/comments?"):
		list = f.comments
	case !strings.HasPrefix(path, "repos/o/r/issues/1/events?"):
		return fmt.Errorf("unexpected path %s", path)
	}
	if !strings.HasSuffix(path, "page=1") {
		list = nil
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (f *fakeDecision) CreateCommentID(_ context.Context, repo string, number int, body string) (int64, error) {
	if repo != "github.com/o/r" || number != 1 {
		return 0, fmt.Errorf("comment on %s#%d", repo, number)
	}
	if f.failPost != nil {
		return 0, f.failPost
	}
	f.posts = append(f.posts, body)
	return f.add(meID, "me", body), nil
}

// add appends a comment one minute after the last one.
func (f *fakeDecision) add(user int64, login, body string) int64 {
	f.nextID++
	f.clock = f.clock.Add(time.Minute)
	at := f.clock.Format(time.RFC3339)
	f.comments = append(f.comments, map[string]any{"id": f.nextID, "user": map[string]any{"id": user, "login": login}, "body": body,
		"html_url": fmt.Sprintf("%s#issuecomment-%d", issueURL, f.nextID), "created_at": at, "updated_at": at})
	return f.nextID
}

const pendingBody = repair.PendingMarker + "\nNibfin stopped and needs a person.\n"

func recordBody(forBody, status, action string) string {
	return repair.RecordMarker + "\n<!-- nibfin:repair-state for=" + repair.Digest(forBody) + " status=" + status + " action=" + action +
		" handled= -->\nNibfin's repair stage looked at the latest `pending` stop.\n\n- Category: E, product decision\n" +
		"- Reason: Two merges conflict.\n- Evidence: code `X`\n- Done: Nothing; `pending` stays.\n- Advice: Choose one option below.\n\n" +
		"### Options\n\n1. **Keep it**\n   It stays.\n   Issue change: None.\n2. **Drop it** (recommended)\n   It goes.\n   Issue change: The sentence goes.\n" +
		"3. **Split it**\n   Two Issues.\n   Issue change: A new Issue."
}

// decisionSetup is a pending Issue in choose with Core's pending comment
// and record, on both fakes.
func decisionSetup(t *testing.T, labels ...string) (*fakeGitHub, *fakeIssue, *fakeDecision) {
	t.Helper()
	g := newFake(t)
	r := g.repo("o/r", "pause", "pending")
	if labels == nil {
		labels = []string{"pending"}
	}
	is := r.issue("o/r", 1, labels...)
	d := &fakeDecision{clock: time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC), nextID: 100,
		events: []map[string]any{{"event": "labeled", "created_at": "2026-11-28T00:00:00Z", "actor": map[string]any{"id": coreID},
			"label": map[string]any{"name": "pending"}}}}
	d.add(coreID, "nibfin", pendingBody)
	d.add(coreID, "nibfin", recordBody(pendingBody, "waiting-for-choice", "options"))
	old := newDecisionAPI
	newDecisionAPI = func(host string) (decisionAPI, error) {
		if host != "github.com" {
			t.Errorf("host = %s", host)
		}
		return d, nil
	}
	t.Cleanup(func() { newDecisionAPI = old })
	return g, is, d
}

func resumeOption(issues []string, n int) Operation {
	return Resume(ResumeParams{IssuesParams: IssuesParams{Repo: "github.com/o/r", Issues: issues}, Option: n})
}

func itemByID(items []Check, id string) Check {
	for _, c := range items {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestResumeOption(t *testing.T) {
	g, is, d := decisionSetup(t, "pause", "pending")
	var events []Event
	ctx := WithProgress(context.Background(), func(e Event) { events = append(events, e) })
	p, res, err := Run(ctx, resumeOption([]string{"1"}, 2), noConfirm())
	if err != nil {
		t.Fatal(err)
	}
	want := Step{ID: "decision:" + issueURL, Action: "add comment", Target: issueURL, Detail: `decision for option 2, "Drop it" (recommended)`,
		Current: "no decision", Next: "option 2 by me"}
	if p.Confirm || len(p.Steps) != 2 || !reflect.DeepEqual(p.Steps[0], want) || p.Steps[1].Detail != "pause, pending" {
		t.Fatalf("plan = %+v", p)
	}
	if !reflect.DeepEqual(d.posts, []string{"<!-- nibfin:decision option=2 -->\nChose option 2."}) {
		t.Errorf("posts = %q", d.posts)
	}
	if len(is.labels) != 0 || !reflect.DeepEqual(g.calls, []string{"query", "mutation", "readback"}) {
		t.Errorf("labels = %v, calls = %v", is.labels, g.calls)
	}
	items := takeItems(t, res)
	if len(items) != 2 || !items[0].Confirmed || items[0].ID != want.ID || items[0].State != "option 2 by me" || !items[1].Confirmed {
		t.Errorf("items = %+v", items)
	}
	if res[issueURL] != "resumed" {
		t.Errorf("result = %v", res)
	}
	var phases []string
	for _, e := range events {
		phases = append(phases, e.ID+" "+e.Phase)
	}
	if want := []string{"decision:" + issueURL + " started", "decision:" + issueURL + " done",
		"issue:" + issueURL + " started", "issue:" + issueURL + " done"}; !reflect.DeepEqual(phases, want) {
		t.Errorf("progress = %v", phases)
	}
}

func TestResumeOptionPlanErrors(t *testing.T) {
	cases := []struct {
		name   string
		issues []string
		n      int
		setup  func(*fakeGitHub, *fakeIssue, *fakeDecision)
		msg    string
	}{
		{name: "two Issues", issues: []string{"1", "2"}, n: 1, msg: "one Issue; 2 were given"},
		{name: "negative", issues: []string{"1"}, n: -1, msg: "--option must be 1 or more"},
		{name: "out of range", issues: []string{"1"}, n: 4, msg: "the options are 1, 2, 3 (recommended: 2)"},
		{name: "no pending", issues: []string{"1"}, n: 1, setup: func(_ *fakeGitHub, is *fakeIssue, _ *fakeDecision) { is.labels = []string{"pause"} },
			msg: "has no pending label"},
		{name: "advice state", issues: []string{"1"}, n: 1, setup: func(_ *fakeGitHub, _ *fakeIssue, d *fakeDecision) {
			d.comments[1]["body"] = strings.Replace(d.comments[1]["body"].(string), "status=waiting-for-choice action=options", "status=needs-person action=advice", 1)
		}, msg: "in the act repair state (Two merges conflict.)"},
		{name: "stale record", issues: []string{"1"}, n: 1, setup: func(_ *fakeGitHub, _ *fakeIssue, d *fakeDecision) {
			d.comments[1]["body"] = recordBody("another stop", "waiting-for-choice", "options")
		}, msg: "in the act repair state (" + repair.NoRecordReason + ")"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, is, d := decisionSetup(t)
			g.repos["o/r"].issue("o/r", 2, "pending")
			if c.setup != nil {
				c.setup(g, is, d)
			}
			_, _, err := Run(context.Background(), resumeOption(c.issues, c.n), noConfirm())
			if Code(err) != CodeInvalidArgument || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("err = %v", err)
			}
			if len(d.posts) != 0 || contains(g.calls, "mutation") {
				t.Errorf("wrote: posts %v, calls %v", d.posts, g.calls)
			}
		})
	}
}

func TestResumeOptionOwnDecisionIsNoChange(t *testing.T) {
	g, is, d := decisionSetup(t)
	d.add(5, "bob", "I prefer 1")
	own := d.add(meID, "me", decisionBody(2))
	o := resumeOption([]string{"1"}, 2)
	p, res, err := Run(context.Background(), o, noConfirm())
	if err != nil {
		t.Fatal(err)
	}
	if !p.Steps[0].NoChange || p.Steps[0].Current != "option 2 by me" || len(d.posts) != 0 || len(is.labels) != 0 {
		t.Fatalf("plan = %+v, posts = %v, labels = %v", p.Steps, d.posts, is.labels)
	}
	if c := itemByID(takeItems(t, res), "decision:"+issueURL); !c.Confirmed || o.(*decisionOp).expect != own {
		t.Errorf("item = %+v", c)
	}
	if !contains(g.calls, "mutation") {
		t.Errorf("calls = %v", g.calls)
	}
}

// A's decision for N, then B's number-only decision: A's rerun writes N
// again, so A's is the newest decision.
func TestResumeOptionRewritesAfterAnotherDecision(t *testing.T) {
	for name, other := range map[string]string{"another number": "3", "the same number": decisionBody(2)} {
		t.Run(name, func(t *testing.T) {
			_, _, d := decisionSetup(t)
			d.add(meID, "me", decisionBody(2))
			d.add(5, "bob", other)
			p, res, err := Run(context.Background(), resumeOption([]string{"1"}, 2), noConfirm())
			if err != nil || p.Steps[0].NoChange || len(d.posts) != 1 {
				t.Fatalf("err %v, plan %+v, posts %v", err, p.Steps, d.posts)
			}
			if c := itemByID(takeItems(t, res), "decision:"+issueURL); !c.Confirmed {
				t.Errorf("item = %+v", c)
			}
			s, _ := repair.Read(context.Background(), d.Get, "repos/o/r/issues/1")
			if c, n, _ := repair.LatestDecision(s.After); n != 2 || c.Author != "me" {
				t.Errorf("latest = %+v %d", c, n)
			}
		})
	}
}

func TestResumeOptionAnotherDecisionBeforeReadBack(t *testing.T) {
	g, is, d := decisionSetup(t)
	g.afterMut = func() { d.add(5, "bob", "1") }
	_, res, err := Run(context.Background(), resumeOption([]string{"1"}, 2), noConfirm())
	if Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "option 1 by bob") {
		t.Fatalf("err = %v", err)
	}
	items := takeItems(t, res)
	c := itemByID(items, "decision:"+issueURL)
	if c.Confirmed || c.Reason != "the newest decision is option 1 by bob" || len(is.labels) != 0 || !itemByID(items, "issue:"+issueURL).Confirmed {
		t.Errorf("item = %+v, labels = %v", c, is.labels)
	}
}

func TestResumeOptionApplyChecks(t *testing.T) {
	t.Run("a newer decision turns the no-change comment into a write", func(t *testing.T) {
		_, _, d := decisionSetup(t)
		d.add(meID, "me", decisionBody(2))
		o := resumeOption([]string{"1"}, 2)
		p, err := o.Plan(context.Background())
		if err != nil || !p.Steps[0].NoChange {
			t.Fatalf("plan %+v, err %v", p, err)
		}
		d.add(5, "bob", "3")
		if err := o.Apply(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		res, err := o.ReadBack(context.Background(), p)
		if err != nil || len(d.posts) != 1 || !itemByID(res[ResultItems].([]Check), "decision:"+issueURL).Confirmed {
			t.Fatalf("res %v, err %v, posts %v", res, err, d.posts)
		}
	})
	t.Run("a changed record writes nothing", func(t *testing.T) {
		for name, change := range map[string]func(*fakeDecision){
			"new stop": func(d *fakeDecision) { d.comments[0]["body"] = pendingBody + "again" },
			"advice":   func(d *fakeDecision) { d.comments[1]["body"] = recordBody(pendingBody, "needs-person", "advice") },
			"pending removed": func(d *fakeDecision) {
				d.events = append(d.events, map[string]any{"event": "unlabeled", "created_at": "2026-11-28T01:00:00Z", "label": map[string]any{"name": "pending"}})
			},
		} {
			g, is, d := decisionSetup(t)
			var events []Event
			ctx := WithProgress(context.Background(), func(e Event) { events = append(events, e) })
			o := resumeOption([]string{"1"}, 2)
			p, err := o.Plan(ctx)
			if err != nil {
				t.Fatal(err)
			}
			change(d)
			err = o.Apply(ctx, p)
			if Code(err) != CodeNotApplied || !strings.Contains(err.Error(), "changed; run gh nibfin status again") {
				t.Fatalf("%s: err = %v", name, err)
			}
			if len(d.posts) != 0 || contains(g.calls, "mutation") || !contains(is.labels, "pending") {
				t.Errorf("%s: wrote: posts %v, calls %v", name, d.posts, g.calls)
			}
			if len(events) != 2 || events[0].Phase != PhaseFailed || events[1].Phase != PhaseFailed || events[1].Code != CodeNotApplied {
				t.Errorf("%s: progress = %+v", name, events)
			}
		}
	})
	t.Run("a failed comment leaves the labels", func(t *testing.T) {
		g, is, d := decisionSetup(t)
		d.failPost = Errorf(CodeGitHubAPIFailed, "comment on o/r#1: HTTP 502")
		var events []Event
		ctx := WithProgress(context.Background(), func(e Event) { events = append(events, e) })
		_, _, err := Run(ctx, resumeOption([]string{"1"}, 2), noConfirm())
		if Code(err) != CodeGitHubAPIFailed || contains(g.calls, "mutation") || !contains(is.labels, "pending") {
			t.Fatalf("err %v, calls %v, labels %v", err, g.calls, is.labels)
		}
		var phases []string
		for _, e := range events {
			phases = append(phases, e.ID+" "+e.Phase)
		}
		if want := []string{"decision:" + issueURL + " started", "decision:" + issueURL + " failed", "issue:" + issueURL + " failed"}; !reflect.DeepEqual(phases, want) {
			t.Errorf("progress = %v", phases)
		}
	})
	t.Run("a failed label mutation after the comment, then a rerun", func(t *testing.T) {
		g, is, d := decisionSetup(t)
		g.failIDs[is.id] = true
		if _, _, err := Run(context.Background(), resumeOption([]string{"1"}, 2), noConfirm()); Code(err) != CodeGitHubError {
			t.Fatalf("err = %v", err)
		}
		if len(d.posts) != 1 || !contains(is.labels, "pending") {
			t.Fatalf("posts %v, labels %v", d.posts, is.labels)
		}
		delete(g.failIDs, is.id)
		p, res, err := Run(context.Background(), resumeOption([]string{"1"}, 2), noConfirm())
		if err != nil || !p.Steps[0].NoChange || len(d.posts) != 1 || len(is.labels) != 0 {
			t.Fatalf("rerun: err %v, plan %+v, posts %v, labels %v", err, p.Steps, d.posts, is.labels)
		}
		for _, c := range takeItems(t, res) {
			if !c.Confirmed {
				t.Errorf("item = %+v", c)
			}
		}
	})
}

func TestResumeOptionReadBackFailure(t *testing.T) {
	_, _, d := decisionSetup(t)
	o := resumeOption([]string{"1"}, 2)
	failing := &failingGet{fakeDecision: d}
	newDecisionAPI = func(string) (decisionAPI, error) { return failing, nil }
	_, res, err := Run(context.Background(), o, noConfirm())
	if err == nil {
		t.Fatal("no error")
	}
	if c := itemByID(res[ResultItems].([]Check), "decision:"+issueURL); c.Confirmed || !strings.HasPrefix(c.Reason, "not read back") {
		t.Errorf("item = %+v", c)
	}
}

// failingGet fails every read after the first two pairs (plan and apply).
type failingGet struct {
	*fakeDecision
	n int
}

func (f *failingGet) Get(ctx context.Context, path string, v any) error {
	f.n++
	if f.n > 4 {
		return errors.New("HTTP 502")
	}
	return f.fakeDecision.Get(ctx, path, v)
}
