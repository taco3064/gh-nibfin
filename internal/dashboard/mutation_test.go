package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/pause"
)

type labelProvider struct {
	reads   atomic.Int32
	edits   atomic.Int32
	inspect func(context.Context, pause.Target) (pause.Facts, error)
	edit    func(context.Context, pause.Target) error
}

func (p *labelProvider) Inspect(ctx context.Context, _ string,
	target pause.Target) (pause.Facts, error) {
	p.reads.Add(1)
	if p.inspect != nil {
		return p.inspect(ctx, target)
	}
	return pause.Facts{User: pause.User{ID: 7, Login: "owner"}, Target: target,
		Title: "<img onerror=secret>", Open: true, Scoped: true, Owner: true}, nil
}
func (p *labelProvider) Edit(ctx context.Context, _ string,
	target pause.Target, _ pause.Action) error {
	p.edits.Add(1)
	if p.edit != nil {
		return p.edit(ctx, target)
	}
	return nil
}

const labelReview = `{"phase":"review","repository":"example/project","action":"pause",
 "batch":true,"targets":[{"repository_id":1,"issue_id":101,"number":1}]}`

func labelRequest(s *Service, body string) *http.Request {
	r := localRequest(s, "/api/labels")
	r.Method = "POST"
	r.Header.Set("Origin", s.URL())
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Body = ioBody(body)
	r.ContentLength = int64(len(body))
	return r
}

func ioBody(body string) *stringBody { return &stringBody{strings.NewReader(body)} }

type stringBody struct{ *strings.Reader }

func (*stringBody) Close() error { return nil }

func serveLabels(s *Service, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, labelRequest(s, body))
	return w
}
func reviewToken(t *testing.T, s *Service, body string) string {
	t.Helper()
	w := serveLabels(s, body)
	var review struct {
		Review string
		Items  []pause.Item
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &review) != nil ||
		len(review.Review) != 64 || len(review.Items) == 0 {
		t.Fatal(w.Code, w.Body)
	}
	return review.Review
}
func applyBody(token string) string {
	return `{"phase":"apply","review":"` + token + `"}`
}

func TestMutationAuthorityAndProofBeforeProvider(t *testing.T) {
	p := &labelProvider{}
	s := launch(t, Config{Controller: pause.Controller{Provider: p}})
	cases := []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"host", func(r *http.Request) { r.Host = "localhost:80" }, 403},
		{"foreign", func(r *http.Request) { r.Header.Set("Origin", "https://evil") }, 403},
		{"null", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"duplicate origin", func(r *http.Request) { r.Header.Add("Origin", s.URL()) }, 403},
		{"cross site", func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}, 403},
		{"duplicate site", func(r *http.Request) {
			r.Header.Add("Sec-Fetch-Site", "same-origin")
		}, 403},
		{"missing", func(r *http.Request) { r.Header.Del(SessionHeader) }, 401},
		{"malformed", func(r *http.Request) { r.Header.Set(SessionHeader, "bad") }, 401},
		{"wrong", func(r *http.Request) {
			r.Header.Set(SessionHeader, strings.Repeat("0", 64))
		}, 401},
		{"duplicate", func(r *http.Request) { r.Header.Add(SessionHeader, proof(s)) }, 401},
		{"get", func(r *http.Request) { r.Method = "GET" }, 405},
		{"put", func(r *http.Request) { r.Method = "PUT" }, 405},
		{"options", func(r *http.Request) { r.Method = "OPTIONS" }, 405},
		{"query", func(r *http.Request) { r.URL.RawQuery = "x=y" }, 400},
		{"type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400},
		{"chunked", func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := labelRequest(s, labelReview)
			tc.change(r)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
	if p.reads.Load() != 0 || p.edits.Load() != 0 {
		t.Fatal("provider reached")
	}
}

func TestMutationRejectsMalformedAndUnboundedIntent(t *testing.T) {
	p := &labelProvider{}
	s := launch(t, Config{Controller: pause.Controller{Provider: p}})
	cases := []string{
		`{}`, `null`, `[]`, labelReview + `{}`, strings.Repeat("x", mutationBodyLimit+1),
		strings.Replace(labelReview, `"pause"`, `"pause; touch /tmp/pwn"`, 1),
		strings.Replace(labelReview, `"example/project"`, `"other/project"`, 1),
		strings.Replace(labelReview, `"example/project"`, `"--repo=x;sh"`, 1),
		strings.Replace(labelReview, `"number":1`, `"number":-1`, 1),
		strings.Replace(labelReview, `"number":1`, `"number":1.0`, 1),
		strings.Replace(labelReview, `"number":1`, `"number":"1 --help"`, 1),
		strings.Replace(labelReview, `"number":1`, `"number":1,"number":2`, 1),
		strings.Replace(labelReview, `"phase":"review"`, `"phase":"review","phase":"apply"`, 1),
		strings.Replace(labelReview, `"batch":true`, `"command":"gh auth token"`, 1),
		strings.Replace(labelReview, `"batch":true`, `"role":"owner"`, 1),
		strings.Replace(labelReview, `"number":1`, `"number":1,"extra":true`, 1),
		strings.Replace(labelReview, `"issue_id":101`, `"issue_id":0`, 1),
		strings.Replace(labelReview, `"repository_id":1`, `"repository_id":0`, 1),
		strings.Replace(labelReview, `[{"repository_id":1,"issue_id":101,"number":1}]`,
			`[]`, 1),
	}
	target := `{"repository_id":1,"issue_id":101,"number":1}`
	cases = append(cases, strings.Replace(labelReview, target, target+","+target, 1))
	cases = append(cases, strings.Replace(labelReview, target,
		strings.TrimSuffix(strings.Repeat(target+",", 51), ","), 1))
	for i, body := range cases {
		w := serveLabels(s, body)
		if w.Code != 400 {
			t.Fatal(i, w.Code, w.Body)
		}
	}
	if p.reads.Load() != 0 || p.edits.Load() != 0 {
		t.Fatal("invalid intent reached provider")
	}
}

func TestReviewedSetReplayAndConcurrentRequests(t *testing.T) {
	p := &labelProvider{}
	entered, release := make(chan struct{}), make(chan struct{})
	p.edit = func(context.Context, pause.Target) error {
		close(entered)
		<-release
		return errors.New("ambiguous secret /host/path")
	}
	s := launch(t, Config{Controller: pause.Controller{Provider: p}})
	token := reviewToken(t, s, labelReview)
	// An apply cannot substitute targets, action or repository into the review.
	w := serveLabels(s, strings.TrimSuffix(applyBody(token), "}")+`,"targets":[]}`)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serveLabels(s, applyBody(token)) }()
	<-entered
	if w := serveLabels(s, applyBody(token)); w.Code != 503 {
		t.Fatal(w.Code)
	}
	close(release)
	w = <-done
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret") ||
		!strings.Contains(w.Body.String(), `"submission":"Unknown"`) {
		t.Fatal(w.Code, w.Body)
	}
	if w := serveLabels(s, applyBody(token)); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if p.edits.Load() != 1 {
		t.Fatal("duplicate edits")
	}
	other := launch(t, Config{Controller: pause.Controller{Provider: p}})
	if w := serveLabels(other, applyBody(token)); w.Code != 409 {
		t.Fatal(w.Code)
	}
}

func TestReviewExpirySessionExpiryAndBoundedStorage(t *testing.T) {
	now := time.Now()
	p := &labelProvider{}
	s := launch(t, Config{Controller: pause.Controller{Provider: p},
		Now: func() time.Time { return now }})
	token := reviewToken(t, s, labelReview)
	now = now.Add(reviewLifetime)
	if w := serveLabels(s, applyBody(token)); w.Code != 409 {
		t.Fatal(w.Code)
	}
	for i := 0; i < maxReviews; i++ {
		reviewToken(t, s, labelReview)
	}
	if w := serveLabels(s, labelReview); w.Code != 503 {
		t.Fatal(w.Code)
	}
	now = now.Add(SessionLifetime)
	if w := serveLabels(s, labelReview); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if p.edits.Load() != 0 {
		t.Fatal("expired edit")
	}
}

func TestBatchKeepsExactTargetsAndMixedResults(t *testing.T) {
	p := &labelProvider{}
	reads := map[int]int{}
	p.inspect = func(_ context.Context, target pause.Target) (pause.Facts, error) {
		reads[target.Number]++
		if target.Number == 3 && reads[3] == 3 {
			return pause.Facts{}, errors.New("offline")
		}
		return pause.Facts{User: pause.User{ID: 7, Login: "owner"}, Target: target,
			Open: target.Number != 4 || reads[4] == 1, Owner: true, Scoped: true,
			Paused: target.Number == 1 && reads[1] >= 3}, nil
	}
	p.edit = func(_ context.Context, target pause.Target) error {
		if target.Number == 2 {
			return pause.Rejected
		}
		return nil
	}
	s := launch(t, Config{Controller: pause.Controller{Provider: p}})
	var targets []string
	for i := 1; i <= 4; i++ {
		targets = append(targets, fmt.Sprintf(
			`{"repository_id":1,"issue_id":%d,"number":%d}`, 100+i, i))
	}
	body := strings.Replace(labelReview,
		`{"repository_id":1,"issue_id":101,"number":1}`, strings.Join(targets, ","), 1)
	token := reviewToken(t, s, body)
	w := serveLabels(s, applyBody(token))
	var response struct{ Results []pause.Result }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal(w.Code, w.Body)
	}
	if len(response.Results) != 4 || p.edits.Load() != 3 {
		t.Fatal(w.Body)
	}
	for i, result := range response.Results {
		if result.Target.Number != i+1 || result.Verdict != "Unknown" {
			t.Fatal(result)
		}
	}
	if response.Results[0].Submission != "Requested" ||
		response.Results[0].Label != "present" ||
		response.Results[1].Submission != "Request failed" ||
		response.Results[2].Submission != "Requested" ||
		response.Results[2].Label != "unreadable" ||
		response.Results[3].Reason != "closed_issue" {
		t.Fatal(w.Body)
	}
}

func TestShutdownCancelsMutationBeforeFurtherTargets(t *testing.T) {
	p := &labelProvider{}
	s := launch(t, Config{Controller: pause.Controller{Provider: p}})
	token := reviewToken(t, s, labelReview)
	entered := make(chan struct{})
	p.inspect = func(ctx context.Context, target pause.Target) (pause.Facts, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing work deadline")
		}
		close(entered)
		<-ctx.Done()
		return pause.Facts{}, ctx.Err()
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serveLabels(s, applyBody(token)) }()
	<-entered
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	response := <-done
	if response.Code != 401 || p.edits.Load() != 0 {
		t.Fatal(response, p.edits.Load())
	}
}

func TestCanonicalRepositoryCaseAndServerLimit(t *testing.T) {
	p := &labelProvider{}
	s := launch(t, Config{Repository: "EXAMPLE/PROJECT",
		Controller: pause.Controller{Provider: p}})
	token := reviewToken(t, s, labelReview)
	if w := serveLabels(s, applyBody(token)); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	code, body := request(t, s, proof(s))
	var capabilities struct {
		Limit int `json:"label_action_limit"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &capabilities) != nil ||
		capabilities.Limit != pause.MaxTargets {
		t.Fatal(code, body)
	}
}

func TestWorkDeadlineCannotOutliveSession(t *testing.T) {
	now := time.Now()
	p := &labelProvider{}
	s := launch(t, Config{Controller: pause.Controller{Provider: p},
		Now: func() time.Time { return now }})
	token := reviewToken(t, s, labelReview)
	// Keep the review current while leaving only a millisecond of session life.
	s.expires = now.Add(time.Millisecond)
	p.inspect = func(ctx context.Context, _ pause.Target) (pause.Facts, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Error("session bound missing")
		}
		<-ctx.Done()
		return pause.Facts{}, ctx.Err()
	}
	w := serveLabels(s, applyBody(token))
	if p.edits.Load() != 0 || !strings.Contains(w.Body.String(), `"submission":"Unknown"`) {
		t.Fatal(p.edits.Load(), w.Code, w.Body)
	}
}
