package dashboard

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/pause"
)

const reviewLifetime = 2 * time.Minute
const maxReviews = 16
const mutationBodyLimit = 32 * 1024

type reviewed struct {
	selection pause.Selection
	expires   time.Time
	used      bool
}

type mutationRequest struct {
	Batch      bool           `json:"batch,omitempty"`
	Phase      string         `json:"phase"`
	Repository string         `json:"repository,omitempty"`
	Action     pause.Action   `json:"action,omitempty"`
	Targets    []pause.Target `json:"targets,omitempty"`
	Review     string         `json:"review,omitempty"`
}

// Reject duplicate object keys, including nested identity keys, before decoding.
func uniqueJSON(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	if delimiter, ok := token.(json.Delim); ok {
		if delimiter != '{' && delimiter != '[' {
			return false
		}
		keys := map[string]bool{}
		for decoder.More() {
			if delimiter == '{' {
				token, err := decoder.Token()
				key, ok := token.(string)
				if err != nil || !ok || keys[key] {
					return false
				}
				keys[key] = true
			}
			if !uniqueJSON(decoder) {
				return false
			}
		}
		_, err = decoder.Token()
		return err == nil
	}
	return true
}

func decodeMutation(w http.ResponseWriter, r *http.Request) (mutationRequest, bool) {
	var request mutationRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mutationBodyLimit))
	if err != nil || len(body) == 0 {
		return request, false
	}
	tokens := json.NewDecoder(bytes.NewReader(body))
	if !uniqueJSON(tokens) {
		return request, false
	}
	if _, err := tokens.Token(); err != io.EOF {
		return request, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return request, false
	}
	return request, true
}

func (s *Service) mutation(w http.ResponseWriter, r *http.Request) {
	if status := s.acquire(r); status != 0 {
		code := "unauthorized"
		if status == 503 {
			code = "unavailable"
		}
		failure(w, status, code)
		return
	}
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	if r.Header.Get("Content-Type") != "application/json" {
		failure(w, 400, "invalid_request")
		return
	}
	request, ok := decodeMutation(w, r)
	if !ok {
		failure(w, 400, "invalid_request")
		return
	}
	bound := min(pause.WorkBound, s.expires.Sub(s.now()))
	ctx, cancel := context.WithTimeout(s.mutationContext, bound)
	defer cancel()
	switch request.Phase {
	case "review":
		intent := pause.Request{Repository: request.Repository, Action: request.Action,
			Targets: request.Targets, Batch: request.Batch}
		if request.Review != "" || !intent.Valid() ||
			!strings.EqualFold(intent.Repository, s.repository) {
			failure(w, 400, "invalid_request")
			return
		}
		for token, entry := range s.reviews {
			if !s.now().Before(entry.expires) {
				delete(s.reviews, token)
			}
		}
		if len(s.reviews) >= maxReviews {
			failure(w, 503, "unavailable")
			return
		}
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			failure(w, 503, "unavailable")
			return
		}
		selection := s.controller.Review(ctx, intent)
		token := hex.EncodeToString(nonce[:])
		expires := s.now().Add(reviewLifetime)
		s.reviews[token] = &reviewed{selection: selection, expires: expires}
		s.mutationResponse(w, struct {
			Review  string       `json:"review"`
			Expires time.Time    `json:"expires"`
			Items   []pause.Item `json:"items"`
		}{token, expires, selection.Items})
	case "apply":
		if request.Batch || request.Repository != "" || request.Action != "" ||
			request.Targets != nil || len(request.Review) != 64 {
			failure(w, 400, "invalid_request")
			return
		}
		entry, ok := s.reviews[request.Review]
		if !ok || entry.used || !s.now().Before(entry.expires) {
			failure(w, 409, "review_unavailable")
			return
		}
		// Consume before external effects, including ambiguous effects and disconnects.
		entry.used = true
		results := s.controller.Apply(ctx, entry.selection)
		s.mutationResponse(w, struct {
			Results []pause.Result `json:"results"`
		}{results})
	default:
		failure(w, 400, "invalid_request")
	}
}

func (s *Service) mutationResponse(w http.ResponseWriter, value any) {
	s.mu.Lock()
	valid := !s.closed && s.now().Before(s.expires)
	s.mu.Unlock()
	if !valid {
		failure(w, 401, "unauthorized")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
