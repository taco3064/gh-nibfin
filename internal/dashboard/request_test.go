package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func localRequest(s *Service, path string) *http.Request {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = strings.TrimPrefix(s.URL(), "http://")
	r.Header.Set(SessionHeader, proof(s))
	return r
}

func TestRequestAuthorityAndFixedSurfaceRejectBeforeCollection(t *testing.T) {
	collector := &recordingCollector{}
	s := launch(t, Config{Provider: collector})
	cases := []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"host name", func(r *http.Request) { r.Host = "localhost:80" }, 403},
		{"host suffix", func(r *http.Request) { r.Host += ".evil.example" }, 403},
		{"port", func(r *http.Request) { r.Host = "127.0.0.1:1" }, 403},
		{"malformed host", func(r *http.Request) { r.Host += "@evil" }, 403},
		{"host whitespace", func(r *http.Request) { r.Host += " " }, 403},
		{"host trailing dot", func(r *http.Request) { r.Host = "127.0.0.1.:80" }, 403},
		{"forwarded rescue", func(r *http.Request) {
			r.Header.Set("Forwarded", "host="+r.Host)
			r.Header.Set("X-Forwarded-Host", r.Host)
			r.Host = "evil.example"
		}, 403},
		{"foreign origin", func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.example")
		}, 403},
		{"origin suffix", func(r *http.Request) { r.Header.Set("Origin", s.URL()+".x") }, 403},
		{"null origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"origin slash", func(r *http.Request) { r.Header.Set("Origin", s.URL()+"/") }, 403},
		{"origin empty", func(r *http.Request) { r.Header["Origin"] = []string{""} }, 403},
		{"origin duplicate", func(r *http.Request) {
			r.Header["Origin"] = []string{s.URL(), s.URL()}
		}, 403},
		{"cross site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
			403},
		{"post", func(r *http.Request) { r.Method = "POST" }, 405},
		{"preflight", func(r *http.Request) { r.Method = "OPTIONS" }, 405},
		{"head", func(r *http.Request) { r.Method = "HEAD" }, 405},
		{"query", func(r *http.Request) { r.URL.RawQuery = "repo=evil/repo" }, 400},
		{"empty query", func(r *http.Request) { r.URL.ForceQuery = true }, 400},
		{"body", func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader("command=rm"))
			r.ContentLength = 10
		}, 400},
		{"chunked", func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }, 400},
		{"missing proof", func(r *http.Request) { r.Header.Del(SessionHeader) }, 401},
		{"guessed proof", func(r *http.Request) {
			r.Header.Set(SessionHeader, strings.Repeat("0", 64))
		}, 401},
		{"malformed proof", func(r *http.Request) {
			r.Header.Set(SessionHeader, strings.Repeat("x", 64))
		}, 401},
		{"short proof", func(r *http.Request) { r.Header.Set(SessionHeader, "a") }, 401},
		{"duplicate proof", func(r *http.Request) {
			r.Header.Add(SessionHeader, proof(s))
		}, 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := localRequest(s, "/api/snapshot")
			tc.change(r)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("CORS granted")
			}
			if !strings.Contains(w.Body.String(), `"error":`) {
				t.Fatal("unsafe error shape")
			}
		})
	}
	for _, path := range []string{"/api/pause", "/api/snapshot/", "/api/snapshot.json",
		"/api/command", "/../go.mod", "/%2e%2e/go.mod", "/assets/../go.mod",
		"//session.js", "/session.js/", "/%73ession.js", "/api%2fsnapshot",
		"/.git/config", "/go.mod", "/assets/", "/session.js%00", "/api/../snapshot"} {
		r := localRequest(s, path)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 400 && w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	if collector.calls.Load() != 0 {
		t.Fatal("rejected request collected")
	}
	for _, origin := range []string{"", s.URL()} {
		r := localRequest(s, "/api/snapshot")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("X-Forwarded-Host", "ignored.evil.example")
		r.Header.Set("X-Forwarded-Proto", "https")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if collector.calls.Load() != 2 {
		t.Fatal("valid requests not collected")
	}
}

func TestStaticAssetsAndProtectiveHeaders(t *testing.T) {
	s := launch(t, Config{})
	for _, tc := range []struct{ path, kind string }{
		{"/", "text/html; charset=utf-8"},
		{"/session.js", "text/javascript; charset=utf-8"},
	} {
		r := localRequest(s, tc.path)
		r.Header.Del(SessionHeader)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Content-Type") != tc.kind {
			t.Fatal(w.Code, w.Header())
		}
		for name, want := range map[string]string{
			"Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
			"X-Content-Type-Options": "nosniff", "Cross-Origin-Resource-Policy": "same-origin",
		} {
			if w.Header().Get(name) != want {
				t.Fatal(name, w.Header())
			}
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") ||
			!strings.Contains(csp, "frame-ancestors 'none'") {
			t.Fatal(csp)
		}
		if strings.Contains(w.Body.String(), proof(s)) ||
			strings.Contains(w.Body.String(), "schema_version") {
			t.Fatal("shell leaked data")
		}
	}
	// In-memory assets cannot change after ownership passes to Start.
	data := []byte("trusted shell")
	assets := map[string]Asset{"/": {"text/html; charset=utf-8", data}}
	custom := launch(t, Config{Assets: assets})
	data[0] = 'X'
	delete(assets, "/")
	r := localRequest(custom, "/")
	r.Header.Del(SessionHeader)
	w := httptest.NewRecorder()
	custom.ServeHTTP(w, r)
	if w.Body.String() != "trusted shell" {
		t.Fatal("mutable asset configuration")
	}
	for _, path := range []string{"../secret", "/../secret", "/a//b", "/a%2fb",
		"/api", "/api/snapshot", "/a?x", "/a\\b"} {
		_, err := copyAssets(map[string]Asset{
			"/":  {"text/html; charset=utf-8", nil},
			path: {"text/html; charset=utf-8", nil},
		})
		if err == nil {
			t.Fatal("unsafe asset path", path)
		}
	}
}

func TestSnapshotAndErrorResponsesNeverCacheOrExposeProof(t *testing.T) {
	s := launch(t, Config{})
	for _, token := range []string{"", proof(s)} {
		r := localRequest(s, "/api/snapshot")
		r.Header.Set(SessionHeader, token)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" ||
			w.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
			strings.Contains(w.Body.String(), proof(s)) {
			t.Fatal("unsafe snapshot/error headers or body")
		}
	}
	r := localRequest(s, "/session.js")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("session header accepted on static resource")
	}
	_, err := copyAssets(map[string]Asset{"/": {"text/plain", nil}})
	if err == nil {
		t.Fatal("unsupported asset content type")
	}
}
