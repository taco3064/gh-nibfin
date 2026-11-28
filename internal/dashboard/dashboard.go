// Package dashboard is the local server behind gh nibfin dashboard. It binds
// 127.0.0.1, admits only the holder of a one-time session proof, and exposes
// every command's plan, confirm, apply and read-back as a JSON API over the
// same operation entry points the CLI calls.
package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// Ops holds the operation entry points. The CLI and the server share one
// value, so both reach exactly the same functions.
type Ops struct {
	RunnerAdd    func(op.RunnerAddParams) op.Operation
	RunnerRemove func(op.RunnerRemoveParams) op.Operation
	Login        func(op.LoginParams) op.Operation
	Init         func(op.RepoParams) op.Operation
	Upgrade      func(op.RepoParams) op.Operation
	Status       func(op.RepoParams) op.Operation
	Pause        func(op.IssuesParams) op.Operation
	Resume       func(op.ResumeParams) op.Operation
}

// Options configure Serve.
type Options struct {
	// Port to listen on; 0 lets the system choose a free one.
	Port int
	Ops  Ops
	// ResolveRepo turns a repository value into HOST/OWNER/REPO; an empty
	// value means the repository of the directory the server started in.
	ResolveRepo func(string) (string, error)
	// Ready is called with the session URL once the server is serving.
	Ready func(sessionURL string)
	// SessionOpened is called when the session proof is exchanged.
	SessionOpened func()
	// Commands is the schema GET /api/commands serves.
	Commands []Command
	// Watch sends the person view and its changes until ctx ends; GET
	// /api/live streams what it sends. Nil leaves the route unavailable.
	Watch func(ctx context.Context, send func(any)) error
	// Activity reads the Runner view's activity; GET /api/runner-activity
	// serves it. part is "" for the full read or "current" for the
	// Runners' current Jobs only (?part=current). Nil leaves the route
	// unavailable.
	Activity func(ctx context.Context, part string) (any, error)
}

// Header names of the session proof and the session.
const (
	ProofHeader   = "X-Nibfin-Proof"
	SessionHeader = "X-Nibfin-Session"
)

// Serve listens on 127.0.0.1 and serves until ctx ends. It returns nil after
// ctx ends and a coded error when it cannot listen or stops serving.
func Serve(ctx context.Context, o Options) error {
	if o.Port < 0 || o.Port > 65535 {
		return op.Errorf(op.CodeInvalidArgument, "--port must be between 0 and 65535, got %d", o.Port)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Port)))
	if err != nil {
		return op.Errorf(op.CodeListenFailed, "cannot listen on 127.0.0.1:%d: %w", o.Port, err)
	}
	s := newServer(o, ln.Addr().(*net.TCPAddr).Port)
	hs := &http.Server{
		Handler: s,
		// Ctrl-C cancels every in-flight plan or apply, as it cancels a CLI
		// command.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	served := make(chan error, 1)
	go func() { served <- hs.Serve(ln) }()
	if o.Ready != nil {
		o.Ready(s.sessionURL())
	}
	select {
	case err := <-served:
		return op.Errorf(op.CodeListenFailed, "the Dashboard server stopped: %w", err)
	case <-ctx.Done():
	}
	if err := hs.Shutdown(context.Background()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return op.Errorf(op.CodeListenFailed, "the Dashboard server did not stop cleanly: %w", err)
	}
	return nil
}

// server holds one run's proof, session and stored plans, all in memory.
type server struct {
	o      Options
	host   string // the exact Host header accepted
	origin string // the exact Origin header accepted
	url    string // the session URL, with the proof in its fragment
	mux    *http.ServeMux

	mu       sync.Mutex
	proof    string // "" once exchanged
	session  string // "" until the exchange
	plans    map[string]*storedPlan
	applying string // the command being applied, or ""
	// liveCancel ends the running live stream; liveID numbers the streams.
	liveCancel context.CancelFunc
	liveID     int
}

func newServer(o Options, port int) *server {
	proof := rand.Text()
	s := &server{
		o:      o,
		host:   fmt.Sprintf("127.0.0.1:%d", port),
		origin: fmt.Sprintf("http://127.0.0.1:%d", port),
		proof:  proof,
		plans:  map[string]*storedPlan{},
	}
	s.url = s.origin + "/#proof=" + proof
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /{$}", s.page)
	s.mux.HandleFunc("POST /api/session", s.exchange)
	s.mux.HandleFunc("GET /api/commands", s.commands)
	s.mux.HandleFunc("POST /api/commands/status/read", s.statusRead)
	s.mux.HandleFunc("POST /api/commands/{command}/plan", s.plan)
	s.mux.HandleFunc("POST /api/commands/{command}/plan/stream", s.planStream)
	s.mux.HandleFunc("POST /api/plans/{id}/apply", s.apply)
	s.mux.HandleFunc("POST /api/plans/{id}/code", s.code)
	s.mux.HandleFunc("DELETE /api/plans/{id}", s.drop)
	s.mux.HandleFunc("GET /api/live", s.live)
	s.mux.HandleFunc("GET /api/runner-activity", s.activity)
	s.mux.HandleFunc("GET /", s.asset)
	s.mux.HandleFunc("/", notFound)
	return s
}

func (s *server) sessionURL() string { return s.url }

// ServeHTTP checks Host, Origin and the session before any route runs.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", apiCSP)

	// An exact Host blocks DNS rebinding; localhost is refused too.
	if r.Host != s.host {
		writeError(w, http.StatusForbidden, op.Errorf(op.CodeForbiddenHost, "Host %q is not %s", r.Host, s.host))
		return
	}
	if origin, ok := r.Header["Origin"]; ok && (len(origin) != 1 || origin[0] != s.origin) {
		writeError(w, http.StatusForbidden, op.Errorf(op.CodeForbiddenOrigin, "Origin %q is not %s", r.Header.Get("Origin"), s.origin))
		return
	}
	// Only the API needs the session: the page and its files carry no data,
	// and the exchange checks the proof itself.
	if isAPI(r.URL.Path) && r.URL.Path != "/api/session" && !s.hasSession(r) {
		writeError(w, http.StatusUnauthorized, op.Errorf(op.CodeSessionRequired, "this request needs the %s header from the session exchange", SessionHeader))
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *server) hasSession(r *http.Request) bool {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	return session != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get(SessionHeader)), []byte(session)) == 1
}

// exchange consumes the proof and opens the run's only session.
func (s *server) exchange(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get(ProofHeader)
	s.mu.Lock()
	if s.proof == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.proof)) != 1 {
		s.mu.Unlock()
		writeError(w, http.StatusUnauthorized, op.Errorf(op.CodeProofInvalid, "the session URL is invalid or was already used; restart gh nibfin dashboard for a new one"))
		return
	}
	s.proof = ""
	s.session = rand.Text()
	session := s.session
	s.mu.Unlock()
	if s.o.SessionOpened != nil {
		s.o.SessionOpened()
	}
	writeJSON(w, http.StatusOK, map[string]string{"session": session})
}
