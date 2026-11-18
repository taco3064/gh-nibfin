package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

func (s *Service) overviewRequest(w http.ResponseWriter, r *http.Request) {
	if status := s.authenticate(r); status != 0 {
		failure(w, status, "unauthorized")
		return
	}
	s.mu.Lock()
	o := s.overview
	if o == nil {
		s.mu.Unlock()
		failure(w, 404, "overview_unavailable")
		return
	}
	if r.URL.Path == "/api/overview" {
		select {
		case o.refresh <- struct{}{}:
		default:
		}
		if len(o.history) == 0 {
			s.mu.Unlock()
			failure(w, 503, "overview_pending")
			return
		}
		data := o.history[len(o.history)-1].data
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
		return
	}
	if o.streams >= 8 {
		s.mu.Unlock()
		failure(w, 503, "stream_limit")
		return
	}
	o.streams++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); o.streams--; s.mu.Unlock() }()
	last := uint64(0)
	if values := r.Header.Values("Last-Event-ID"); len(values) > 0 {
		var err error
		last, err = strconv.ParseUint(values[0], 10, 64)
		if len(values) != 1 || err != nil || len(values[0]) > 20 {
			failure(w, 400, "invalid_cursor")
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	// Each stream ends well before RequestBound's HTTP WriteTimeout. Every write
	// also has a short deadline, so a stalled reader cannot freeze shutdown.
	end := time.NewTimer(time.Minute)
	defer end.Stop()
	heartbeat := time.NewTicker(5 * time.Second)
	defer heartbeat.Stop()
	for {
		if s.authenticate(r) != 0 {
			return
		}
		s.mu.Lock()
		changed := o.changed
		events := overviewEvents(o.history, last)
		s.mu.Unlock()
		for _, event := range events {
			if s.authenticate(r) != 0 {
				return
			}
			if !writeOverview(w, fmt.Sprintf("id: %d\nevent: reset\ndata: %s\n\n",
				event.id, event.data)) {
				return
			}
			last = event.id
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.mutationContext.Done():
			return
		case <-end.C:
			return
		case <-changed:
		case <-heartbeat.C:
			if !writeOverview(w, ": heartbeat\n\n") {
				return
			}
		}
	}
}

func overviewEvents(history []overviewEvent, last uint64) []overviewEvent {
	if len(history) == 0 {
		return nil
	}
	latest := history[len(history)-1]
	if last == 0 || last > latest.id || last < history[0].id {
		return []overviewEvent{latest}
	}
	for index, event := range history {
		if event.id > last {
			return append([]overviewEvent(nil), history[index:]...)
		}
	}
	return nil
}

func writeOverview(w http.ResponseWriter, data string) bool {
	controller := http.NewResponseController(w)
	if controller.SetWriteDeadline(time.Now().Add(5*time.Second)) != nil {
		return false
	}
	if _, err := io.WriteString(w, data); err != nil {
		return false
	}
	return controller.Flush() == nil
}

// Repository selection shares the existing snapshot/mutation slot. The allowlist
// is populated exclusively by permission-checked adopted repository observations.
func (s *Service) selectRepository(w http.ResponseWriter, r *http.Request) {
	if status := s.acquire(r); status != 0 {
		failure(w, status, "unavailable_or_unauthorized")
		return
	}
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	var input struct {
		Repository string `json:"repository"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512))
	tokens := json.NewDecoder(bytes.NewReader(body))
	if err != nil || !uniqueJSON(tokens) {
		failure(w, 400, "invalid_request")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if r.Header.Get("Content-Type") != "application/json" ||
		decoder.Decode(&input) != nil || !snapshot.ValidRepository(input.Repository) {
		failure(w, 400, "invalid_repository")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		failure(w, 400, "invalid_request")
		return
	}
	s.mu.Lock()
	allowed := false
	if s.overview != nil {
		for _, repo := range s.overview.current.Repositories.Items {
			allowed = allowed || strings.EqualFold(repo.Name, input.Repository)
		}
	}
	if allowed {
		s.repository = input.Repository
		// A reviewed operation cannot cross repository navigation.
		s.reviews = make(map[string]*reviewed)
	}
	s.mu.Unlock()
	if !allowed {
		failure(w, 403, "repository_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"selected":true}`)
}
