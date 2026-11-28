package op

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
	"github.com/taco3064/gh-nibfin/internal/repair"
)

// decisionAPI is the part of the REST API resume --option and --reply use,
// with the current gh login.
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

// replyBody is the comment a reply posts: the text trimmed as JavaScript
// trims, inner line endings as given.
func replyBody(reply string) string {
	return strings.TrimFunc(reply, func(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF })
}

// decisionOp is resume --option N or --reply TEXT: it answers the repair
// record that covers the Issue's stop with a decision comment (R4.51,
// R9.54), then removes pause and pending as resume does. --option N writes
// the marked decision for option N, which Core maps to that option;
// --reply writes the person's own words as one ordinary comment without a
// marker, which Core hands to Shape as written.
type decisionOp struct {
	label  *labelOp
	option int
	// reply is --reply as given; "" for --option.
	reply string

	// Resolved by Plan.
	planned bool
	ref     issueRef
	url     string
	api     decisionAPI
	user    ghUser
	record  repair.Report
	// body is the comment the decision posts.
	body string
	// skip is the current user's own comment that already is the planned
	// decision and the newest decision candidate after the record; 0 when
	// the comment is written.
	skip int64
	// expect is the decision comment the read-back looks for: the one
	// Apply wrote, or skip.
	expect int64
}

func (d *decisionOp) issuePath() string {
	return fmt.Sprintf("repos/%s/%s/issues/%d", d.ref.Owner, d.ref.Name, d.ref.Number)
}

func (d *decisionOp) repo() string { return d.ref.Host + "/" + d.ref.Owner + "/" + d.ref.Name }

// flag is the flag that gives the decision.
func (d *decisionOp) flag() string {
	if d.reply != "" {
		return "--reply"
	}
	return "--option"
}

// matches reports whether comment c, whose option ParseDecision read as
// option, is the current user's planned decision: the same option for
// --option, the same text (line endings aside) for --reply.
func (d *decisionOp) matches(c repair.Comment, option int) bool {
	if c.AuthorID != d.user.ID {
		return false
	}
	if d.reply != "" {
		return repair.Normalize(c.Body) == repair.Normalize(d.body)
	}
	return option == d.option
}

// ownLatest is the id of the newest decision candidate after the record
// when it is the current user's planned decision; 0 otherwise.
func (d *decisionOp) ownLatest(s repair.Snapshot) int64 {
	if c, n, ok := s.Candidate(); ok && d.matches(c, n) {
		return c.ID
	}
	return 0
}

// printable drops control characters from record text, which is partly
// agent-written, before it reaches a terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// printableText is printable for a text of several lines: it keeps the
// line breaks.
func printableText(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\r\n", "\n"))
}

// newest describes the newest decision candidate after the record, the
// comment Core takes as the decision when its author can write.
func newest(s repair.Snapshot) string {
	c, n, ok := s.Candidate()
	switch {
	case !ok:
		return "no decision"
	case n == 0:
		return "a reply by " + c.Author
	}
	return fmt.Sprintf("option %d by %s", n, c.Author)
}

// Plan checks the decision (exactly one of --option N, N ≥ 1, and a reply
// that is not blank) and that there is exactly one Issue, plans the label
// change as resume does, and reads the repair state: it must be choose,
// with option N for --option. The comment is a no-change item only when
// the newest decision candidate after the record is the current user's own
// planned decision.
func (d *decisionOp) Plan(ctx context.Context) (Plan, error) {
	p := Plan{Operation: d.label.name}
	switch {
	case d.option != 0 && d.reply != "":
		return p, Errorf(CodeInvalidArgument, "--option and --reply cannot be used together; answer with one of them")
	case d.reply != "":
		if d.body = replyBody(d.reply); d.body == "" {
			return p, Errorf(CodeInvalidArgument, "--reply is empty; write the decision in your own words")
		}
	case d.option < 1:
		return p, Errorf(CodeInvalidArgument, "--option must be 1 or more, got %d", d.option)
	default:
		d.body = decisionBody(d.option)
	}
	refs, err := parseIssueRefs(d.label.params.Repo, d.label.params.Issues)
	if err != nil {
		return p, err
	}
	if len(refs) != 1 {
		return p, Errorf(CodeInvalidArgument, "%s answers one Issue; %d were given", d.flag(), len(refs))
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
		return p, Errorf(CodeInvalidArgument, "%s has no pending label, so there is no repair record to answer; use resume without %s", d.url, d.flag())
	}
	if rep.State != repair.StateChoose {
		return p, Errorf(CodeInvalidArgument, "%s is in the %s repair state (%s), not choose; only choose has options to answer", d.url, rep.State, printable(rep.Reason))
	}
	if _, ok := optionTitle(rep, d.option); d.reply == "" && !ok {
		return p, Errorf(CodeInvalidArgument, "%d is not an option of %s; the options are %s", d.option, d.url, optionList(rep))
	}
	d.record = rep
	d.skip = d.ownLatest(s)

	st := d.planStep()
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

// planStep is the plan item of the decision comment as planned. For
// --reply its Detail is the exact comment body, with control characters
// other than line breaks dropped for the terminal.
func (d *decisionOp) planStep() Step {
	st := Step{ID: "decision:" + d.url, Action: "add comment", Target: d.url}
	if d.reply != "" {
		st.Detail = printableText(d.body)
		// Core reads a reply that is only a number as that option.
		st.Next = "a reply by " + d.user.Login
		if n, ok := repair.ParseDecision(d.body); ok {
			st.Next = fmt.Sprintf("option %d by %s", n, d.user.Login)
		}
		return st
	}
	title, _ := optionTitle(d.record, d.option)
	st.Detail = "decision for " + title
	st.Next = "option " + strconv.Itoa(d.option) + " by " + d.user.Login
	return st
}

// Apply reads the repair state again and writes nothing when the record
// changed. Then it writes the decision comment, unless the current user's
// own planned decision is still the newest candidate, and removes the
// labels as resume does. A failed comment leaves the labels alone.
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
	if _, ok := optionTitle(rep, d.option); d.reply == "" && !ok {
		return notStarted(Errorf(CodeNotApplied, "the repair record of %s no longer has option %d; run gh nibfin status again", d.url, d.option))
	}
	d.expect = d.ownLatest(s)
	if d.expect == 0 {
		err := Track(ctx, st, func() error {
			id, err := d.api.CreateCommentID(ctx, d.repo(), d.ref.Number, d.body)
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
// decision is confirmed only when the newest decision candidate after the
// record, by anyone, is the expected comment of the current user with the
// planned option or text.
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
		c, n, ok := s.Candidate()
		if ok && c.ID == d.expect && d.matches(c, n) {
			checks.Set(st, true, newest(s), "")
		} else {
			reason := "the newest decision is " + newest(s)
			checks.Set(st, false, newest(s), reason)
			err = Errorf(CodeNotApplied, "%s: your decision on %s is not the newest decision: %s", d.label.name, d.url, reason)
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
