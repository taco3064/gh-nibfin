package ghretry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// quiet replaces Log and Sleep for one test and records the waits.
func quiet(t *testing.T) (*bytes.Buffer, *[]time.Duration) {
	t.Helper()
	var log bytes.Buffer
	var waits []time.Duration
	oldLog, oldSleep := Log, Sleep
	Log = &log
	Sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	t.Cleanup(func() { Log, Sleep = oldLog, oldSleep })
	return &log, &waits
}

// server answers with codes in order, then with the last one.
func server(t *testing.T, codes ...int) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var n atomic.Int32
	var bodies []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		code := codes[min(i, len(codes)-1)]
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"n":`+string(rune('0'+i))+`}`)
	}))
	t.Cleanup(s.Close)
	return s, &n, &bodies
}

func do(t *testing.T, ctx context.Context, method, url, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestSucceedsOnTheThirdRetry(t *testing.T) {
	log, waits := quiet(t)
	s, n, bodies := server(t, 502, 403, 422, 201)
	resp := do(t, context.Background(), http.MethodPost, s.URL+"/repos/a/b/labels?secret=x", `{"name":"scope"}`)
	if resp.StatusCode != 201 || n.Load() != 4 {
		t.Fatalf("status %d after %d attempts, want 201 after 4", resp.StatusCode, n.Load())
	}
	for _, b := range *bodies {
		if b != `{"name":"scope"}` {
			t.Fatalf("a retry sent body %q", b)
		}
	}
	if got := *waits; len(got) != 3 || got[0] != time.Second || got[1] != 2*time.Second || got[2] != 4*time.Second {
		t.Fatalf("waits %v", got)
	}
	want := "nibfin: GitHub API POST /repos/a/b/labels failed (HTTP 502); retry 1 of 3\n" +
		"nibfin: GitHub API POST /repos/a/b/labels failed (HTTP 403); retry 2 of 3\n" +
		"nibfin: GitHub API POST /repos/a/b/labels failed (HTTP 422); retry 3 of 3\n"
	if log.String() != want {
		t.Fatalf("log %q", log.String())
	}
}

func TestFailsAfterThreeRetries(t *testing.T) {
	quiet(t)
	s, n, _ := server(t, 500)
	resp := do(t, context.Background(), http.MethodGet, s.URL+"/x", "")
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || n.Load() != 4 || string(b) != `{"n":3}` {
		t.Fatalf("status %d, %d attempts, body %s", resp.StatusCode, n.Load(), b)
	}
}

func TestAnswersThatAreNotErrors(t *testing.T) {
	quiet(t)
	for _, c := range []struct {
		method string
		code   int
	}{{http.MethodGet, 404}, {http.MethodHead, 410}, {http.MethodDelete, 404}, {http.MethodGet, 304}, {http.MethodGet, 200}} {
		s, n, _ := server(t, c.code)
		resp := do(t, context.Background(), c.method, s.URL+"/x", "")
		if resp.StatusCode != c.code || n.Load() != 1 {
			t.Errorf("%s %d: %d attempts", c.method, c.code, n.Load())
		}
	}
	// A 404 on a write is an error like any other.
	s, n, _ := server(t, 404, 204)
	if resp := do(t, context.Background(), http.MethodPut, s.URL+"/x", "{}"); resp.StatusCode != 204 || n.Load() != 2 {
		t.Fatalf("PUT 404: status %d after %d attempts", resp.StatusCode, n.Load())
	}
}

func TestPlanGatedForbiddenIsAnAnswer(t *testing.T) {
	quiet(t)
	for _, c := range []struct {
		msg   string
		tries int32
	}{
		{"Upgrade to GitHub Pro or make this repository public to enable this feature.", 1},
		{"API rate limit exceeded", 4},
	} {
		var n atomic.Int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"`+c.msg+`"}`)
		}))
		resp := do(t, context.Background(), http.MethodGet, s.URL+"/repos/o/r/rules/branches/main", "")
		b, _ := io.ReadAll(resp.Body)
		s.Close()
		if n.Load() != c.tries || !strings.Contains(string(b), c.msg) {
			t.Errorf("%q: %d attempts, body %s", c.msg, n.Load(), b)
		}
	}
}

func TestGraphQLErrorsAreRetried(t *testing.T) {
	log, _ := quiet(t)
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"errors":[{"message":"Something went wrong"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"ok":true}}`)
	}))
	defer s.Close()
	resp := do(t, context.Background(), http.MethodPost, s.URL+"/api/graphql", `{"query":"{ok}"}`)
	b, _ := io.ReadAll(resp.Body)
	if string(b) != `{"data":{"ok":true}}` || n.Load() != 2 || !strings.Contains(log.String(), "(GraphQL error); retry 1 of 3") {
		t.Fatalf("body %s after %d attempts, log %q", b, n.Load(), log.String())
	}
}

func TestGraphQLNotFoundIsAnAnswer(t *testing.T) {
	quiet(t)
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		_, _ = io.WriteString(w, `{"data":{"a":null},"errors":[{"type":"NOT_FOUND","path":["a"]}]}`)
	}))
	defer s.Close()
	if resp := do(t, context.Background(), http.MethodPost, s.URL+"/graphql", `{}`); resp.StatusCode != 200 || n.Load() != 1 {
		t.Fatalf("%d attempts", n.Load())
	}
	// Not JSON is the caller's to judge.
	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		_, _ = io.WriteString(w, `<html>`)
	}))
	defer s2.Close()
	if resp := do(t, context.Background(), http.MethodPost, s2.URL+"/graphql", `{}`); resp.StatusCode != 200 || n.Load() != 2 {
		t.Fatalf("%d attempts", n.Load())
	}
}

func TestNoResponseIsRetried(t *testing.T) {
	log, _ := quiet(t)
	var n int
	rt := Transport(roundTrip(func(r *http.Request) (*http.Response, error) {
		n++
		if n < 3 {
			return nil, errors.New("dial tcp: connection refused " + r.URL.String())
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/x?token=abc", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil || resp.StatusCode != 200 || n != 3 {
		t.Fatalf("err %v after %d attempts", err, n)
	}
	if strings.Contains(log.String(), "token") || !strings.Contains(log.String(), "GET /x failed (no response); retry 2 of 3") {
		t.Fatalf("log %q", log.String())
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWaitsAsGitHubAsks(t *testing.T) {
	now = func() time.Time { return time.Unix(1000, 0) }
	t.Cleanup(func() { now = time.Now })
	for _, c := range []struct {
		h    http.Header
		want time.Duration
	}{
		{http.Header{"Retry-After": {"7"}}, 7 * time.Second},
		{http.Header{"Retry-After": {time.Unix(1030, 0).UTC().Format(http.TimeFormat)}}, 30 * time.Second},
		{http.Header{"Retry-After": {"3600"}}, maxWait},
		{http.Header{"Retry-After": {"soon"}}, 2 * time.Second},
		{http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1005"}}, 5 * time.Second},
		{http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"990"}}, 0},
		{http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"x"}}, 2 * time.Second},
		{http.Header{"X-Ratelimit-Remaining": {"12"}}, 2 * time.Second},
	} {
		if got := delay(2, &http.Response{Header: c.h}); got != c.want {
			t.Errorf("%v: wait %v, want %v", c.h, got, c.want)
		}
	}
}

func TestOnceIsSentOneTime(t *testing.T) {
	quiet(t)
	s, n, _ := server(t, 502)
	resp := do(t, Once(context.Background()), http.MethodPost, s.URL+"/issues", "{}")
	if resp.StatusCode != 502 || n.Load() != 1 {
		t.Fatalf("status %d after %d attempts", resp.StatusCode, n.Load())
	}
}

func TestDoDoesNotDuplicateACreate(t *testing.T) {
	log, _ := quiet(t)
	sends, reads := 0, 0
	err := Do(context.Background(), http.MethodPost, "/repos/a/b/issues", func(ctx context.Context) error {
		if ctx.Value(onceKey{}) != true {
			t.Fatal("send is not marked Once")
		}
		sends++
		return errors.New("HTTP 502")
	}, func(ctx context.Context) (bool, error) {
		if ctx.Value(onceKey{}) != nil {
			t.Fatal("the re-read is marked Once")
		}
		reads++
		return reads == 2, nil
	})
	if err != nil || sends != 2 || reads != 2 {
		t.Fatalf("err %v, %d sends, %d reads", err, sends, reads)
	}
	if !strings.Contains(log.String(), "POST /repos/a/b/issues failed (error); retry 2 of 3") {
		t.Fatalf("log %q", log.String())
	}
}

func TestDoGivesUp(t *testing.T) {
	quiet(t)
	last := errors.New("last")
	sends := 0
	err := Do(context.Background(), http.MethodPost, "/x", func(context.Context) error {
		sends++
		return last
	}, func(context.Context) (bool, error) { return false, nil })
	if err != last || sends != 4 {
		t.Fatalf("err %v after %d sends", err, sends)
	}
	// A failed re-read stops without sending again.
	sends = 0
	err = Do(context.Background(), http.MethodPost, "/x", func(context.Context) error {
		sends++
		return last
	}, func(context.Context) (bool, error) { return false, errors.New("read") })
	if err != last || sends != 1 {
		t.Fatalf("err %v after %d sends", err, sends)
	}
	// So does a cancelled wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sends = 0
	if err := Do(ctx, http.MethodPost, "/x", func(context.Context) error { sends++; return last }, nil); err != last || sends != 1 {
		t.Fatalf("err %v after %d sends", err, sends)
	}
}

func TestCancelledWaitStopsTheTransport(t *testing.T) {
	quiet(t)
	s, n, _ := server(t, 500)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	_, err := Transport(http.DefaultTransport).RoundTrip(req)
	if err == nil {
		t.Fatalf("no error after %d attempts", n.Load())
	}
}

func TestBodyThatCannotBeResent(t *testing.T) {
	quiet(t)
	s, n, _ := server(t, 500)
	req, _ := http.NewRequest(http.MethodPost, s.URL, io.NopCloser(strings.NewReader("x")))
	if _, err := Transport(nil).RoundTrip(req); err == nil || !strings.Contains(err.Error(), "cannot be sent again") || n.Load() != 1 {
		t.Fatalf("err %v after %d attempts", err, n.Load())
	}
}

func TestRealSleep(t *testing.T) {
	if err := Sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Sleep(ctx, time.Hour); err == nil {
		t.Fatal("no error from a cancelled sleep")
	}
}
