package repair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The testdata records were written by nibfin-core 6711257's
// formatRepairRecord and fenced (src/stages/repair-record.ts, prompt.ts)
// with its optionsDetails and stepsDetails (repair-agent.ts);
// pending-778.md and record-778.md are the live comments 7225880335 and
// 7225892958 of taco3064/nibfin-core#778.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDigestLive(t *testing.T) {
	if got := Digest(fixture(t, "pending-778.md")); got != "c8770c02565cb645" {
		t.Fatalf("Digest = %s, want c8770c02565cb645", got)
	}
	s, ok := ParseState(fixture(t, "record-778.md"))
	want := State{For: "c8770c02565cb645", Status: "needs-person", Action: "advice", Handled: []string{}}
	if !ok || !reflect.DeepEqual(s, want) {
		t.Fatalf("ParseState = %+v %v, want %+v", s, ok, want)
	}
	tx := ParseText(fixture(t, "record-778.md"))
	if tx.Category != "not classified" || tx.Reason != "The repair stage cannot decide the code `STAGE_BLOCKED` by itself yet." ||
		tx.Done != "Nothing; `pending` stays." || !strings.HasPrefix(tx.Advice, "Read the latest Nibfin comment") ||
		tx.Options != nil || tx.Steps != nil || tx.BugReport != nil {
		t.Fatalf("ParseText = %+v", tx)
	}
}

func TestParseState(t *testing.T) {
	const line = "<!-- nibfin:repair-state for=0123456789abcdef status=repairing action=%s handled=%s%s -->"
	cases := []struct {
		name, body string
		ok         bool
		want       State
	}{
		{"marker then state", RecordMarker + "\n" + fmt.Sprintf(line, "retry", "A_B,C", "") + "\ntext", true,
			State{For: "0123456789abcdef", Status: "repairing", Action: "retry", Handled: []string{"A_B", "C"}}},
		{"state first", fmt.Sprintf(line, "wait", "", " reset=2026-11-28T05:00:00Z"), true,
			State{For: "0123456789abcdef", Status: "repairing", Action: "wait", Handled: []string{}, Reset: "2026-11-28T05:00:00Z"}},
		{"every agent action", fmt.Sprintf(line, "rerun", "NONE", ""), true,
			State{For: "0123456789abcdef", Status: "repairing", Action: "rerun", Handled: []string{"NONE"}}},
		{"wait without reset", fmt.Sprintf(line, "wait", "", ""), false, State{}},
		{"unknown action", fmt.Sprintf(line, "fix", "", ""), false, State{}},
		{"unknown status", strings.Replace(fmt.Sprintf(line, "retry", "", ""), "repairing", "done", 1), false, State{}},
		{"short digest", strings.Replace(fmt.Sprintf(line, "retry", "", ""), "0123456789abcdef", "0123", 1), false, State{}},
		{"state on the third line", RecordMarker + "\n\n" + fmt.Sprintf(line, "retry", "", ""), false, State{}},
		{"CRLF", RecordMarker + "\r\n" + fmt.Sprintf(line, "retry", "", ""), false, State{}},
		{"empty", "", false, State{}},
	}
	for _, c := range cases {
		got, ok := ParseState(c.body)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ParseState = %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
	for _, a := range []string{"retry", "advice", "agent", "rerun", "amend", "options"} {
		if _, ok := ParseState(fmt.Sprintf(line, a, "", "")); !ok {
			t.Errorf("action %s is rejected", a)
		}
	}
}

func TestParseTextOptions(t *testing.T) {
	tx := ParseText(fixture(t, "choose.md"))
	if tx.Category != "E, product decision" || tx.Reason != "The Issue needs a choice between two merges." ||
		tx.Done != "Nothing; `pending` stays." || tx.Advice != "Choose one option below." {
		t.Fatalf("visible lines = %+v", tx)
	}
	want := []Option{
		{Number: 1, Title: "Keep the `pause` confirmation", Consequence: "The Owner confirms the merge.\n\nReview waits for the Owner.",
			IssueChange: "The merge sentence gains the `pause` part.\nThe Inputs list #780."},
		{Number: 2, Title: "Drop it **now**", Consequence: "The PR merges on a clean review.",
			IssueChange: "The merge sentence loses the `pause` part.", Recommended: true},
		{Number: 3, Title: "Split the Issue", Consequence: "Two smaller PRs.", IssueChange: "Scope opens a new Issue."},
	}
	if !reflect.DeepEqual(tx.Options, want) {
		t.Fatalf("Options =\n%#v\nwant\n%#v", tx.Options, want)
	}
	if !ValidOptions(tx.Options) {
		t.Fatal("the options are not valid")
	}
}

func TestParseTextSteps(t *testing.T) {
	tx := ParseText(fixture(t, "steps.md"))
	want := []Step{
		{Text: "Read the failed run.", Link: "https://github.com/o/r/actions/runs/1"},
		{Text: "Upgrade the workflows: then rerun.", Command: "gh nibfin upgrade -R o/r\n\necho ```done```"},
		{Text: "Remove the `pending` label."},
	}
	if !reflect.DeepEqual(tx.Steps, want) {
		t.Fatalf("Steps =\n%#v\nwant\n%#v", tx.Steps, want)
	}
	wantBug := &BugReport{URL: "https://github.com/taco3064/nibfin-core/issues/new", Title: "Route fails on the same step",
		Body: "## What happened\n\n```text\nboom\n```\n\n### Not a heading"}
	if !reflect.DeepEqual(tx.BugReport, wantBug) {
		t.Fatalf("BugReport = %#v", tx.BugReport)
	}
	if tx.Options != nil || tx.Category != "B, Nibfin process error" {
		t.Fatalf("ParseText = %+v", tx)
	}
	// CRLF bodies parse the same.
	if crlf := ParseText(strings.ReplaceAll(fixture(t, "steps.md"), "\n", "\r\n")); !reflect.DeepEqual(crlf, tx) {
		t.Fatalf("CRLF ParseText = %+v", crlf)
	}
}

func TestValidOptions(t *testing.T) {
	o := func(n int, rec bool) Option { return Option{Number: n, Title: "t", Recommended: rec} }
	cases := []struct {
		name string
		opts []Option
		ok   bool
	}{
		{"two", []Option{o(1, true), o(2, false)}, true},
		{"four", []Option{o(1, false), o(2, false), o(3, true), o(4, false)}, true},
		{"one", []Option{o(1, true)}, false},
		{"five", []Option{o(1, true), o(2, false), o(3, false), o(4, false), o(5, false)}, false},
		{"none recommended", []Option{o(1, false), o(2, false)}, false},
		{"two recommended", []Option{o(1, true), o(2, true)}, false},
		{"gap", []Option{o(1, true), o(3, false)}, false},
		{"no title", []Option{o(1, true), {Number: 2}}, false},
	}
	for _, c := range cases {
		if got := ValidOptions(c.opts); got != c.ok {
			t.Errorf("%s: ValidOptions = %v", c.name, got)
		}
	}
}

func TestDecision(t *testing.T) {
	if got := FormatDecision(2); got != "<!-- nibfin:decision option=2 -->" {
		t.Fatalf("FormatDecision = %q", got)
	}
	cases := []struct {
		body string
		n    int
		ok   bool
	}{
		{"<!-- nibfin:decision option=2 -->", 2, true},
		{"<!-- nibfin:decision option=3 -->\nChose option 3.", 3, true},
		{"  <!-- nibfin:decision option=1 -->  \r\nmore", 1, true},
		{" 3 \n", 3, true},
		{"3\nbecause", 0, false},
		{"0", 0, false},
		{"03", 0, false},
		{"<!-- nibfin:decision option=0 -->", 0, false},
		{"text\n<!-- nibfin:decision option=2 -->", 0, false},
		{"99999999999999999999", 0, false},
		{"9007199254740992", 0, false},
		{"9007199254740991", 9007199254740991, true},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := ParseDecision(c.body)
		if n != c.n || ok != c.ok {
			t.Errorf("ParseDecision(%q) = %d %v, want %d %v", c.body, n, ok, c.n, c.ok)
		}
	}
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestLatestDecision(t *testing.T) {
	cs := []Comment{
		{ID: 10, Author: "a", Body: FormatDecision(1), CreatedAt: at("2026-11-28T01:00:00Z")},
		{ID: 12, Author: "b", Body: "3", CreatedAt: at("2026-11-28T02:00:00Z")},
		{ID: 11, Author: "c", Body: "I think 2", CreatedAt: at("2026-11-28T03:00:00Z")},
	}
	c, n, ok := LatestDecision(cs)
	if !ok || c.ID != 12 || n != 3 {
		t.Fatalf("LatestDecision = %+v %d %v", c, n, ok)
	}
	// The same time: the higher id is newer.
	cs = append(cs, Comment{ID: 13, Author: "a", Body: FormatDecision(2), CreatedAt: at("2026-11-28T02:00:00Z")})
	if c, n, _ := LatestDecision(cs); c.ID != 13 || n != 2 {
		t.Fatalf("LatestDecision = %+v %d", c, n)
	}
	if _, _, ok := LatestDecision([]Comment{{Body: "hi"}}); ok {
		t.Fatal("a comment without a decision is a decision")
	}
}

// fakeAPI serves events and comments pages.
type fakeAPI struct {
	events, comments []map[string]any
	fail             string
	paths            []string
}

func (f *fakeAPI) get(_ context.Context, p string, v any) error {
	f.paths = append(f.paths, p)
	if f.fail != "" && strings.Contains(p, f.fail) {
		return errors.New("HTTP 502")
	}
	list := f.events
	if strings.Contains(p, "/comments") {
		list = f.comments
	}
	page := 1
	fmt.Sscanf(p[strings.LastIndex(p, "page=")+5:], "%d", &page)
	lo, hi := (page-1)*perPage, page*perPage
	if lo > len(list) {
		lo = len(list)
	}
	if hi > len(list) {
		hi = len(list)
	}
	out := make([]map[string]any, hi-lo)
	copy(out, list[lo:hi])
	// Round-trip through the JSON shapes Read decodes.
	return roundTrip(out, v)
}

func event(kind, label string, actor int64, created string) map[string]any {
	return map[string]any{"event": kind, "created_at": created, "actor": map[string]any{"id": actor}, "label": map[string]any{"name": label}}
}

func comment(id, actor int64, login, body, created, updated string) map[string]any {
	return map[string]any{"id": id, "user": map[string]any{"id": actor, "login": login}, "body": body,
		"html_url": fmt.Sprintf("https://github.com/o/r/issues/7#issuecomment-%d", id), "created_at": created, "updated_at": updated}
}

const core = 99

func TestReadAndEvaluate(t *testing.T) {
	pending := fixture(t, "pending.md")
	choose := fixture(t, "choose.md")
	other := strings.Replace(choose, "for="+Digest(pending), "for=0000000000000000", 1)
	base := func() *fakeAPI {
		return &fakeAPI{
			events: []map[string]any{
				event("labeled", "pending", 5, "2026-11-27T00:00:00Z"),
				event("unlabeled", "pending", 5, "2026-11-27T01:00:00Z"),
				event("labeled", "pending", core, "2026-11-28T00:00:00Z"),
				event("labeled", "bug", 6, "2026-11-28T00:30:00Z"),
			},
			comments: []map[string]any{
				comment(1, 6, "mallory", choose, "2026-11-27T23:00:00Z", "2026-11-27T23:00:00Z"),
				comment(2, core, "nibfin", pending, "2026-11-28T00:00:00Z", "2026-11-28T00:00:00Z"),
				comment(3, core, "nibfin", choose, "2026-11-28T00:01:00Z", "2026-11-28T00:05:00Z"),
				comment(4, 7, "alice", FormatDecision(2), "2026-11-28T00:10:00Z", "2026-11-28T00:10:00Z"),
				comment(5, core, "nibfin", "<!-- nibfin:pending -->\nlater copy", "2026-11-28T00:11:00Z", "2026-11-28T00:11:00Z"),
			},
		}
	}

	f := base()
	s, err := Read(context.Background(), f.get, "repos/o/r/issues/7")
	if err != nil {
		t.Fatal(err)
	}
	if s.Actor != core || s.Pending == nil || s.Pending.ID != 2 || s.Record == nil || s.Record.ID != 3 ||
		!s.Unlabeled.Equal(at("2026-11-27T01:00:00Z")) || len(s.After) != 2 || s.After[0].ID != 4 || s.After[0].Author != "alice" {
		t.Fatalf("Read = %+v", s)
	}
	if !s.Record.UpdatedAt.Equal(at("2026-11-28T00:05:00Z")) || s.Record.URL != "https://github.com/o/r/issues/7#issuecomment-3" {
		t.Fatalf("record = %+v", s.Record)
	}
	r, ok := s.Evaluate(true)
	if !ok || r.State != StateChoose || len(r.Text.Options) != 3 || r.Reason != "The Issue needs a choice between two merges." || r.Record.ID != 3 {
		t.Fatalf("Evaluate = %+v", r)
	}
	if _, ok := s.Evaluate(false); ok {
		t.Fatal("an Issue without pending has a state")
	}

	// A mismatched digest: act with the fixed reason and no record.
	f = base()
	f.comments[2] = comment(3, core, "nibfin", other, "2026-11-28T00:01:00Z", "2026-11-28T00:05:00Z")
	s, _ = Read(context.Background(), f.get, "repos/o/r/issues/7")
	if r, _ := s.Evaluate(true); r.State != StateAct || r.Reason != NoRecordReason || r.Record != nil || r.Text.Category != "" {
		t.Fatalf("mismatched digest = %+v", r)
	}

	// A stale pending comment: pending was removed after it last changed.
	f = base()
	f.events = append(f.events, event("unlabeled", "pending", 5, "2026-11-28T00:20:00Z"))
	s, _ = Read(context.Background(), f.get, "repos/o/r/issues/7")
	if r, _ := s.Evaluate(true); r.State != StateAct || r.Reason != NoRecordReason {
		t.Fatalf("stale pending comment = %+v", r)
	}

	// A record only by another account counts for nothing.
	f = base()
	f.comments = []map[string]any{f.comments[0], f.comments[1]}
	s, _ = Read(context.Background(), f.get, "repos/o/r/issues/7")
	if s.Record != nil || len(s.After) != 0 {
		t.Fatalf("look-alike record = %+v", s)
	}
	if r, _ := s.Evaluate(true); r.State != StateAct || r.Reason != NoRecordReason {
		t.Fatalf("look-alike record = %+v", r)
	}

	// No Nibfin comment at all (pending added by hand).
	f = base()
	f.comments = nil
	s, _ = Read(context.Background(), f.get, "repos/o/r/issues/7")
	if r, _ := s.Evaluate(true); r.State != StateAct || r.Reason != NoRecordReason {
		t.Fatalf("no comment = %+v", r)
	}

	// Choose with invalid options degrades to act and keeps the text.
	f = base()
	bad := strings.Replace(choose, "** (recommended)", "**", 1)
	f.comments[2] = comment(3, core, "nibfin", bad, "2026-11-28T00:01:00Z", "2026-11-28T00:05:00Z")
	s, _ = Read(context.Background(), f.get, "repos/o/r/issues/7")
	if r, _ := s.Evaluate(true); r.State != StateAct || r.Text.Options != nil || r.Text.Category != "E, product decision" || r.Record == nil {
		t.Fatalf("invalid options = %+v", r)
	}

	// Repairing and advice records.
	for name, want := range map[string]string{"wait.md": StateRepairing, "steps.md": StateAct} {
		f = base()
		f.comments[2] = comment(3, core, "nibfin", fixture(t, name), "2026-11-28T00:01:00Z", "2026-11-28T00:05:00Z")
		s, _ = Read(context.Background(), f.get, "repos/o/r/issues/7")
		r, _ := s.Evaluate(true)
		if r.State != want || r.Record == nil {
			t.Fatalf("%s = %+v", name, r)
		}
		if name == "steps.md" && (len(r.Text.Steps) != 3 || r.Text.BugReport == nil) {
			t.Fatalf("%s = %+v", name, r)
		}
	}
}

func TestReadPagesAndErrors(t *testing.T) {
	f := &fakeAPI{}
	for i := 0; i < perPage; i++ {
		f.events = append(f.events, event("labeled", "bug", 1, "2026-11-28T00:00:00Z"))
	}
	f.events = append(f.events, event("labeled", "pending", core, "2026-11-28T00:00:00Z"))
	for i := 0; i < perPage; i++ {
		f.comments = append(f.comments, comment(int64(i+1), 1, "x", "hi", "2026-11-28T00:00:00Z", "2026-11-28T00:00:00Z"))
	}
	f.comments = append(f.comments, comment(500, core, "nibfin", PendingMarker+"\nx", "2026-11-28T00:00:00Z", "2026-11-28T00:00:00Z"))
	s, err := Read(context.Background(), f.get, "repos/o/r/issues/7")
	if err != nil || s.Actor != core || s.Pending == nil || s.Pending.ID != 500 {
		t.Fatalf("Read = %+v %v (%v)", s, err, f.paths)
	}
	if len(f.paths) != 4 || f.paths[1] != "repos/o/r/issues/7/events?per_page=100&page=2" {
		t.Fatalf("paths = %v", f.paths)
	}
	for _, part := range []string{"events", "comments"} {
		f.fail = "/" + part
		_, err := Read(context.Background(), f.get, "repos/o/r/issues/7")
		var re *ReadError
		if !errors.As(err, &re) || re.Part != part {
			t.Fatalf("%s failure = %v", part, err)
		}
	}
}

func roundTrip(in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
