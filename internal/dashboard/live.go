package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// live streams the Watch events as Server-Sent Events: each event is one
// "data: <json>" frame. The session travels in the X-Nibfin-Session header,
// which ServeHTTP checks, never in the URL; the page reads the stream with
// fetch, since EventSource cannot send headers. A session has one stream: a
// new one ends the one before. The stream ends when the page leaves or the
// server stops.
func (s *server) live(w http.ResponseWriter, r *http.Request) {
	if s.o.Watch == nil {
		writeError(w, http.StatusNotFound, op.Errorf(op.CodeNotFound, "live updates are not available"))
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	s.mu.Lock()
	if s.liveCancel != nil {
		s.liveCancel()
	}
	s.liveID++
	id := s.liveID
	s.liveCancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.liveID == id {
			s.liveCancel = nil
		}
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	f := &frames{w: w, rc: http.NewResponseController(w)}
	f.flush()
	if err := s.o.Watch(ctx, f.send); err != nil && ctx.Err() == nil {
		f.send(map[string]any{"event": "error", "source": "status", "error": op.AsError(err)})
	}
}

// frames writes Server-Sent Events frames, flushing each one.
type frames struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (f *frames) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(map[string]any{"event": "error", "source": "status", "error": op.AsError(err)})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// JSON has no raw newline, so one data line carries the whole event.
	_, _ = f.w.Write([]byte("data: "))
	_, _ = f.w.Write(b)
	_, _ = f.w.Write([]byte("\n\n"))
	_ = f.rc.Flush()
}

func (f *frames) flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.rc.Flush()
}

// activity serves the Runner view's activity read. It only reads.
func (s *server) activity(w http.ResponseWriter, r *http.Request) {
	if s.o.Activity == nil {
		writeError(w, http.StatusNotFound, op.Errorf(op.CodeNotFound, "Runner activity is not available"))
		return
	}
	v, err := s.o.Activity(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
