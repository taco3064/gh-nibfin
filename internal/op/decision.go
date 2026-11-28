package op

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
	"github.com/taco3064/gh-nibfin/internal/repair"
)

// decisionAPI is the part of the REST API resume --option uses, with the
// current gh login.
type decisionAPI interface {
	User(ctx context.Context) (ghUser, error)
	// Get reads one REST path into v.
	Get(ctx context.Context, path string, v any) error
	// CreateCommentID comments once (R4.38) and returns the comment's id.
	CreateCommentID(ctx context.Context, repo string, number int, body string) (int64, error)
}

// newDecisionAPI returns the REST client of host; tests replace it.
var newDecisionAPI = func(host string) (decisionAPI, error) {
	c, err := api.NewRESTClient(api.ClientOptions{Host: host, Transport: ghretry.Transport(nil)})
	if err != nil {
		return nil, Errorf(CodeAuthUnavailable, "no gh login for %s: %v", host, err)
	}
	return restGitHub{c}, nil
}

func (g restGitHub) Get(ctx context.Context, path string, v any) error {
	return g.c.DoWithContext(ctx, http.MethodGet, path, nil, v)
}

// decisionBody is the decision comment for option n: Core's marker line
// and one fixed sentence. It quotes nothing from the record.
func decisionBody(n int) string {
	return repair.FormatDecision(n) + "\nChose option " + strconv.Itoa(n) + "."
}

// decisionOp is resume --option N: it answers the options of the repair
// record that covers the Issue's stop with a marked decision comment
// (R4.51, R9.54), then removes pause and pending as resume does.
type decisionOp struct {
	label  *labelOp
	option int

	// Resolved by Plan.
	planned bool
	ref     issueRef
	url     string
	api     decisionAPI
	user    ghUser
	record  repair.Report
	// skip is the current user's own decision for option, the newest
	// decision after the record; 0 when the comment is written.
	skip int64
	// expect is the decision comment the read-back looks for: the one
	// Apply wrote, or skip.
	expect int64
}

func (d *decisionOp) issuePath() string {
	return fmt.Sprintf("repos/%s/%s/issues/%d", d.ref.Owner, d.ref.Name, d.ref.Number)
}

func (d *decisionOp) repo() string { return d.ref.Host + "/" + d.ref.Owner + "/" + d.ref.Name }

// ownLatest is the id of the newest decision after the record when the
// current user wrote it for d.option; 0 otherwise.
func (d *decisionOp) ownLatest(s repair.Snapshot) int64 {
	c, n, ok := repair.LatestDecision(s.After)
	if ok && n == d.option && c.AuthorID == d.user.ID {
		return c.ID
	}
	return 0
}

// newest describes the newest decision after the record.
func newest(s repair.Snapshot) string {
	c, n, ok := repair.LatestDecision(s.After)
	if !ok {
		return "no decision"
	}
	return fmt.Sprintf("option %d by %s", n, c.Author)
}

// Plan checks there is exactly one Issue, plans the label change as resume
// does, and reads the repair state: it must be choose with option N. The
// comment is a no-change item only when the newest decision after the
// record is the current user's own for N.
func (d *decisionOp) Plan(ctx context.Context) (Plan, error) {
	p := Plan{Operation: d.label.name}
	if d.option < 1 {
		return p, Errorf(CodeInvalidArgument, "--option must be 1 or more, got %d", d.option)
	}
	refs, err := parseIssueRefs(d.label.params.Repo, d.label.params.Issues)
	if err != nil {
		return p, err
	}
	if len(refs) != 1 {
		return p, Errorf(CodeInvalidArgument, "--option answers one Issue; %d were given", len(refs))
	}
	p, err = d.label.Plan(ctx)
	if err != nil {
		return p, err
	}
	d.ref = refs[0]
	is := d.label.issues[0]
	d.url = is.url
	pending := false
	for _, n := range is.names {
		pending = pending || strings.EqualFold(n, "pending")
	}
	if d.api, err = newDecisionAPI(d.ref.Host); err != nil {
		return p, err
	}
	if d.user, err = d.api.User(ctx); err != nil {
		return p, err
	}
	s, err := d.read(ctx)
	if err != nil {
		return p, err
	}
	rep, ok := s.Evaluate(pending)
	if !ok {
		return p, Errorf(CodeInvalidArgument, "%s has no pending label, so there are no options to answer; use resume without --option", d.url)
	}
	if rep.State != repair.StateChoose {
		return p, Errorf(CodeInvalidArgument, "%s is in the %s repair state (%s), not choose; only choose has options to answer", d.url, rep.State, rep.Reason)
	}
	title, ok := optionTitle(rep, d.option)
	if !ok {
		return p, Errorf(CodeInvalidArgument, "%d is not an option of %s; the options are %s", d.option, d.url, optionList(rep))
	}
	d.record = rep
	d.skip = d.ownLatest(s)

	st := d.commentStep(title)
	st.Current = newest(s)
	if d.skip != 0 {
		st.NoChange = true
		st.Detail = "already your newest decision: " + st.Detail
	}
	p.Steps = append([]Step{st}, p.Steps...)
	d.planned = true
	return p, nil
}

func (d *decisionOp) read(ctx context.Context) (repair.Snapshot, error) {
	s, err := repair.Read(ctx, d.api.Get, d.issuePath())
	if err != nil {
		return s, apiError(fmt.Sprintf("read the repair state of %s", d.url), err)
	}
	return s, nil
}

func optionTitle(rep repair.Report, n int) (string, bool) {
	for _, o := range rep.Text.Options {
		if o.Number == n {
			t := fmt.Sprintf("option %d, %q", n, o.Title)
			if o.Recommended {
				t += " (recommended)"
			}
			return t, true
		}
	}
	return "", false
}

func optionList(rep repair.Report) string {
	var nums []string
	rec := ""
	for _, o := range rep.Text.Options {
		nums = append(nums, strconv.Itoa(o.Number))
		if o.Recommended {
			rec = strconv.Itoa(o.Number)
		}
	}
	return strings.Join(nums, ", ") + " (recommended: " + rec + ")"
}

// commentStep is the plan item of the decision comment.
func (d *decisionOp) commentStep(title string) Step {
	return Step{ID: "decision:" + d.url, Action: "add comment", Target: d.url, Detail: "decision for " + title,
		Next: "option " + strconv.Itoa(d.option) + " by " + d.user.Login}
}

// planStep is the plan item of the comment as planned.
func (d *decisionOp) planStep() Step {
	title, _ := optionTitle(d.record, d.option)
	return d.commentStep(title)
}

// Apply reads the repair state again and writes nothing when the record
// changed. Then it writes the decision comment, unless the current user's
// own decision for N is still the newest, and removes the labels as resume
// does. A failed comment leaves the labels alone.
func (d *decisionOp) Apply(ctx context.Context, p Plan) error {
	if !d.planned {
		return fmt.Errorf("%s: apply before plan", d.label.name)
	}
	st := d.planStep()
	labels := Changes(p.Steps[1:])
	notStarted := func(err error) error {
		Failed(ctx, st, err)
		for _, l := range labels {
			Failed(ctx, l, Errorf(CodeNotApplied, "not started: %v", err))
		}
		return err
	}
	s, err := d.read(ctx)
	if err != nil {
		return notStarted(err)
	}
	rep, ok := s.Evaluate(true)
	if !ok || rep.State != repair.StateChoose || rep.Record == nil || rep.Record.ID != d.record.Record.ID ||
		rep.RecordState.For != d.record.RecordState.For {
		return notStarted(Errorf(CodeNotApplied, "the repair record of %s changed; run gh nibfin status again", d.url))
	}
	if _, ok := optionTitle(rep, d.option); !ok {
		return notStarted(Errorf(CodeNotApplied, "the repair record of %s no longer has option %d; run gh nibfin status again", d.url, d.option))
	}
	d.expect = d.ownLatest(s)
	if d.expect == 0 {
		err := Track(ctx, st, func() error {
			id, err := d.api.CreateCommentID(ctx, d.repo(), d.ref.Number, decisionBody(d.option))
			d.expect = id
			return err
		})
		if err != nil {
			for _, l := range labels {
				Failed(ctx, l, Errorf(CodeNotApplied, "not started: the decision comment failed"))
			}
			return err
		}
	}
	return d.label.Apply(ctx, p)
}

// ReadBack reads the labels back as resume does, then the comments: the
// decision is confirmed only when the newest decision after the record, by
// anyone, is the expected comment of the current user for N.
func (d *decisionOp) ReadBack(ctx context.Context, p Plan) (Result, error) {
	if !d.planned {
		return nil, fmt.Errorf("%s: read back before plan", d.label.name)
	}
	r, labelErr := d.label.ReadBack(ctx, p)
	var checks Checks
	st := d.planStep()
	checks.Expect(st)
	var err error
	s, rerr := d.read(ctx)
	switch {
	case rerr != nil:
		err = rerr
	default:
		c, n, ok := repair.LatestDecision(s.After)
		if ok && c.ID == d.expect && n == d.option && c.AuthorID == d.user.ID {
			checks.Set(st, true, newest(s), "")
		} else {
			reason := "the newest decision is " + newest(s)
			checks.Set(st, false, newest(s), reason)
			err = Errorf(CodeNotApplied, "%s: your decision for option %d on %s is not the newest decision: %s", d.label.name, d.option, d.url, reason)
		}
	}
	items := checks.List(err)
	if r == nil {
		r = Result{}
	}
	if rest, ok := r[ResultItems].([]Check); ok {
		items = append(items, rest...)
	}
	r[ResultItems] = items
	if _, ok := r[ResultNotes]; !ok {
		r[ResultNotes] = []string{}
	}
	if labelErr != nil {
		return r, labelErr
	}
	return r, err
}
