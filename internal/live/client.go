package live

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// answer is one conditional GET's answer: a 200 with its body or a 304.
type answer struct {
	status int
	etag   string
	// date is GitHub's Date header; zero when missing.
	date time.Time
	// poll is the x-poll-interval GitHub asks for; zero when absent.
	poll time.Duration
	body []byte
}

// getter sends a conditional GET of path with If-None-Match etag (none when
// empty). A 2xx or a 304 is an answer; anything else is the client's error.
type getter interface {
	get(ctx context.Context, path, etag string) (answer, error)
}

// newGetter returns a getter for host that carries the current gh login.
// Tests replace it.
var newGetter = func(host string) (getter, error) {
	return restGetterFor(api.ClientOptions{Host: host}, http.DefaultTransport)
}

// maxBody bounds a probe answer.
const maxBody = 32 << 20

// etagKey carries a request's If-None-Match value through go-gh's client.
type etagKey struct{}

// conditional sets If-None-Match from the request context. It sits below
// go-gh's auth and header round-tripper, so the request it sends still
// carries the gh login's Authorization, which a free 304 needs.
type conditional struct{ rt http.RoundTripper }

func (c conditional) RoundTrip(req *http.Request) (*http.Response, error) {
	if etag, _ := req.Context().Value(etagKey{}).(string); etag != "" {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", etag)
	}
	return c.rt.RoundTrip(req)
}

type restGetter struct{ c *api.RESTClient }

// restGetterFor builds go-gh's REST client with the conditional transport
// over rt. go-gh keeps host, API URL and token resolution.
func restGetterFor(o api.ClientOptions, rt http.RoundTripper) (getter, error) {
	o.Transport = conditional{rt: rt}
	c, err := api.NewRESTClient(o)
	if err != nil {
		return nil, err
	}
	return restGetter{c: c}, nil
}

func (g restGetter) get(ctx context.Context, path, etag string) (answer, error) {
	resp, err := g.c.RequestWithContext(context.WithValue(ctx, etagKey{}, etag), http.MethodGet, path, nil)
	if err != nil {
		var h *api.HTTPError
		if errors.As(err, &h) && h.StatusCode == http.StatusNotModified {
			return answerOf(h.StatusCode, h.Headers, nil), nil
		}
		return answer{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return answer{}, err
	}
	return answerOf(resp.StatusCode, resp.Header, body), nil
}

func answerOf(status int, h http.Header, body []byte) answer {
	a := answer{status: status, etag: h.Get("ETag"), body: body}
	if t, err := http.ParseTime(h.Get("Date")); err == nil {
		a.date = t
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("X-Poll-Interval"))); err == nil && n > 0 {
		a.poll = time.Duration(n) * time.Second
	}
	return a
}

// retryAfter is a failed answer's Retry-After, in seconds or as a date;
// zero when absent.
func retryAfter(err error, now time.Time) time.Duration {
	var h *api.HTTPError
	if !errors.As(err, &h) {
		return 0
	}
	v := strings.TrimSpace(h.Headers.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

func notFound(err error) bool {
	var h *api.HTTPError
	return errors.As(err, &h) && (h.StatusCode == http.StatusNotFound || h.StatusCode == http.StatusGone)
}

// checkErr keeps go-gh's status and message; a token never appears in
// either.
func checkErr(what string, err error) *op.Error {
	var h *api.HTTPError
	if errors.As(err, &h) {
		msg := h.Message
		if msg == "" {
			msg = http.StatusText(h.StatusCode)
		}
		return op.AsError(op.Errorf(op.CodeGitHubAPIFailed, "check %s: HTTP %d: %s", what, h.StatusCode, msg))
	}
	return op.AsError(op.Errorf(op.CodeGitHubAPIFailed, "check %s: %v", what, err))
}
