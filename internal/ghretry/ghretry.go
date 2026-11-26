// Package ghretry retries GitHub API calls (Proposal R4.37–R4.39): on any
// error a request is sent again up to 3 times, whatever the error is (no
// response, 5xx, 4xx, rate limit or a GraphQL error), before the caller sees
// the last answer unchanged.
//
// Answers that are not errors are not retried: a GET, HEAD or DELETE 404 or
// 410, the 403 of a plan without the feature and a GraphQL answer whose only
// errors are NOT_FOUND, which callers read as "absent", and a 304. A request marked with Once is
// sent one time only; its caller re-reads before sending it again, so a
// create is never duplicated.
package ghretry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Retries is the number of retries after the first attempt.
const Retries = 3

// maxWait bounds one wait taken from Retry-After or x-ratelimit-reset.
const maxWait = time.Minute

// Log receives one line per retry. Tests replace it.
var Log io.Writer = os.Stderr

// Sleep waits d or until ctx ends. Tests replace it.
var Sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var now = time.Now

type onceKey struct{}

// Once marks the requests sent with ctx to be sent one time only by the
// Transport. The caller retries them itself with Do, re-reading first.
func Once(ctx context.Context) context.Context {
	return context.WithValue(ctx, onceKey{}, true)
}

// Transport wraps rt with the retry rule.
func Transport(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return transport{rt: rt}
}

type transport struct{ rt http.RoundTripper }

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if once, _ := req.Context().Value(onceKey{}).(bool); once {
		return t.rt.RoundTrip(req)
	}
	for attempt := 1; ; attempt++ {
		send := req
		if attempt > 1 {
			var err error
			if send, err = rewind(req); err != nil {
				return nil, err
			}
		}
		resp, err := t.rt.RoundTrip(send)
		reason, failed := failure(req, resp, err)
		if !failed || attempt > Retries {
			return resp, err
		}
		wait := delay(attempt, resp)
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
		}
		Note(req.Method, req.URL.Path, reason, attempt)
		if err := Sleep(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// rewind is req with a fresh copy of its body for another attempt.
func rewind(req *http.Request) (*http.Request, error) {
	again := req.Clone(req.Context())
	if req.Body == nil || req.Body == http.NoBody {
		return again, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("ghretry: %s %s: the request body cannot be sent again", req.Method, req.URL.Path)
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	again.Body = body
	return again, nil
}

// failure tells whether an attempt failed and names how, without the error
// text, which can carry a query string.
func failure(req *http.Request, resp *http.Response, err error) (string, bool) {
	if err != nil {
		return "no response", true
	}
	code := resp.StatusCode
	switch {
	case code == http.StatusNotModified:
		return "", false
	case (code == http.StatusNotFound || code == http.StatusGone) &&
		(req.Method == http.MethodGet || req.Method == http.MethodHead || req.Method == http.MethodDelete):
		return "", false
	case code == http.StatusForbidden && planGated(resp):
		return "", false
	case code >= 400:
		return "HTTP " + strconv.Itoa(code), true
	case code == http.StatusOK && strings.HasSuffix(req.URL.Path, "/graphql"):
		if graphQLErrors(resp) {
			return "GraphQL error", true
		}
	}
	return "", false
}

// planGated reads a 403 and puts it back; it reports whether it is the
// answer GitHub gives on a plan without the feature ("Upgrade to GitHub Pro
// or make this repository public to enable this feature."), which callers
// read as "absent".
func planGated(resp *http.Response) bool {
	body := keep(resp)
	var answer struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	msg := strings.ToLower(answer.Message)
	return strings.Contains(msg, "upgrade to github pro") || strings.Contains(msg, "make this repository public")
}

// keep reads resp's body and puts a copy back for the caller.
func keep(resp *http.Response) []byte {
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

// graphQLErrors reads a GraphQL answer and puts it back; it reports whether
// the answer carries a top-level error other than NOT_FOUND, which callers
// read as "absent" like a REST 404.
func graphQLErrors(resp *http.Response) bool {
	body := keep(resp)
	var answer struct {
		Errors []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	for _, e := range answer.Errors {
		if e.Type != "NOT_FOUND" {
			return true
		}
	}
	return false
}

// delay is GitHub's Retry-After or x-ratelimit-reset when the answer has one
// (at most maxWait), otherwise 1 s, 2 s, 4 s.
func delay(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := asked(resp.Header); ok {
			return d
		}
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func asked(h http.Header) (time.Duration, bool) {
	var d time.Duration
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			d = time.Duration(n) * time.Second
		} else if t, err := http.ParseTime(v); err == nil {
			d = t.Sub(now())
		} else {
			return 0, false
		}
	} else if h.Get("X-Ratelimit-Remaining") == "0" {
		n, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-Ratelimit-Reset")), 10, 64)
		if err != nil {
			return 0, false
		}
		d = time.Unix(n, 0).Sub(now())
	} else {
		return 0, false
	}
	return min(max(d, 0), maxWait), true
}

// Note writes the retry line: method, URL path and attempt, never the query
// string, a body or a header.
func Note(method, path, reason string, attempt int) {
	fmt.Fprintf(Log, "nibfin: GitHub API %s %s failed (%s); retry %d of %d\n", method, path, reason, attempt, Retries)
}

// Do runs send for a request marked with Once: after a failed send it waits,
// calls done to see whether the first attempt took effect after all, and only
// sends again when it did not, up to Retries times. The last error is
// returned unchanged, also when the re-read fails.
func Do(ctx context.Context, method, path string, send func(context.Context) error, done func(context.Context) (bool, error)) error {
	once := Once(ctx)
	for attempt := 1; ; attempt++ {
		err := send(once)
		if err == nil || attempt > Retries {
			return err
		}
		Note(method, path, "error", attempt)
		if waitErr := Sleep(ctx, delay(attempt, nil)); waitErr != nil {
			return err
		}
		// Without a re-read the first attempt may have taken effect, so it is
		// not sent again.
		ok, readErr := done(ctx)
		if readErr != nil {
			return err
		}
		if ok {
			return nil
		}
	}
}
