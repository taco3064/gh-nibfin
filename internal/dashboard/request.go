package dashboard

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/pause"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

func protectiveHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; "+
		"connect-src 'self'; style-src 'self'; img-src 'self'; "+
		"base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
}

func failure(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{code})
}

// authority checks do not consult Forwarded or X-Forwarded-* headers.
func (s *Service) authority(r *http.Request) bool {
	if r.Host != strings.TrimPrefix(s.origin, "http://") {
		return false
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 || len(origins) == 1 && origins[0] != s.origin {
		return false
	}
	sites := r.Header.Values("Sec-Fetch-Site")
	if len(sites) > 1 {
		return false
	}
	if len(sites) == 1 && sites[0] != "same-origin" && sites[0] != "none" {
		return false
	}
	return true
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	protectiveHeaders(w.Header())
	if !s.authority(r) {
		failure(w, 403, "forbidden")
		return
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		failure(w, 503, "unavailable")
		return
	}
	if r.URL.Path == "/api/labels" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			failure(w, 405, "method_not_allowed")
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" ||
			r.RequestURI != r.URL.Path || len(r.TransferEncoding) != 0 ||
			r.ContentLength > mutationBodyLimit {
			failure(w, 400, "invalid_request")
			return
		}
		s.mutation(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		failure(w, 405, "method_not_allowed")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" ||
		r.RequestURI != r.URL.Path || r.ContentLength != 0 ||
		len(r.TransferEncoding) != 0 {
		failure(w, 400, "invalid_request")
		return
	}
	if r.URL.Path == "/api/snapshot" {
		s.snapshot(w, r)
		return
	}
	if asset, ok := s.assets[r.URL.Path]; ok {
		// Session proof belongs only on the data API, never static asset requests.
		if len(r.Header.Values(SessionHeader)) != 0 {
			failure(w, 400, "invalid_request")
			return
		}
		w.Header().Set("Content-Type", asset.ContentType)
		_, _ = w.Write(asset.Data)
		return
	}
	failure(w, 404, "not_found")
}

func (s *Service) acquire(r *http.Request) int {
	proof := r.Header.Values(SessionHeader)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 503
	}
	if len(proof) != 1 || len(proof[0]) != 64 || !s.now().Before(s.expires) {
		return 401
	}
	if _, err := hex.DecodeString(proof[0]); err != nil ||
		subtle.ConstantTimeCompare([]byte(proof[0]), []byte(s.secret)) != 1 {
		return 401
	}
	if s.busy {
		return 503
	}
	s.busy = true
	return 0
}

func (s *Service) snapshot(w http.ResponseWriter, r *http.Request) {
	if status := s.acquire(r); status != 0 {
		code := "unauthorized"
		if status == 503 {
			code = "unavailable"
		}
		failure(w, status, code)
		return
	}
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	// Collection is synchronous. Client disconnects cannot release the single
	// collection slot while a non-context-aware transport is still working.
	result := s.provider.Collect(s.repository).At(s.now())
	data, err := json.Marshal(struct {
		snapshot.Snapshot
		MaxTargets int `json:"label_action_limit"`
	}{result, pause.MaxTargets})
	if err != nil {
		failure(w, 500, "unavailable")
		return
	}
	// Shutdown or session expiry during collection must not disclose its result.
	s.mu.Lock()
	valid := !s.closed && s.now().Before(s.expires)
	s.mu.Unlock()
	if !valid {
		failure(w, 401, "unauthorized")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}
