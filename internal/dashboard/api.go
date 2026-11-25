package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// maxBody limits every request body.
const maxBody = 1 << 20

// storedPlan is a plan waiting for its apply. The Operation value is kept,
// since the operations hold state from Plan that Apply needs.
type storedPlan struct {
	command  string
	o        op.Operation
	p        op.Plan
	interact *interactor
	busy     bool // being applied
}

// build decodes a command's params, defaults and resolves them as the CLI
// handler does, and returns the operation.
func (s *server) build(command string, r *http.Request, ia *interactor) (op.Operation, int, error) {
	ops := s.o.Ops
	resolve := func(repo string) (string, error) { return s.o.ResolveRepo(repo) }
	switch command {
	case "runner-add":
		var p op.RunnerAddParams
		if err := decode(r, &p); err != nil {
			return nil, http.StatusBadRequest, err
		}
		if p.Count == 0 {
			p.Count = op.DefaultRunnerAddCount
		}
		p.Interact = ia
		return ops.RunnerAdd(p), 0, nil
	case "runner-remove":
		var p op.RunnerRemoveParams
		if err := decode(r, &p); err != nil {
			return nil, http.StatusBadRequest, err
		}
		return ops.RunnerRemove(p), 0, nil
	case "login":
		var p op.LoginParams
		if err := decode(r, &p); err != nil {
			return nil, http.StatusBadRequest, err
		}
		p.Interact = ia
		return ops.Login(p), 0, nil
	case "init", "upgrade", "status":
		var p op.RepoParams
		if err := decode(r, &p); err != nil {
			return nil, http.StatusBadRequest, err
		}
		// Without a repository status gives the person view; init and
		// upgrade use the directory the server started in.
		if command != "status" || p.Repo != "" {
			repo, err := resolve(p.Repo)
			if err != nil {
				return nil, http.StatusUnprocessableEntity, err
			}
			p.Repo = repo
		}
		switch command {
		case "init":
			return ops.Init(p), 0, nil
		case "upgrade":
			return ops.Upgrade(p), 0, nil
		}
		return ops.Status(p), 0, nil
	case "pause", "resume":
		var p op.IssuesParams
		if err := decode(r, &p); err != nil {
			return nil, http.StatusBadRequest, err
		}
		// Only numbers need a repository.
		if p.Repo != "" || op.HasIssueNumber(p.Issues) {
			repo, err := resolve(p.Repo)
			if err != nil {
				return nil, http.StatusUnprocessableEntity, err
			}
			p.Repo = repo
		}
		if command == "pause" {
			return ops.Pause(p), 0, nil
		}
		return ops.Resume(p), 0, nil
	}
	return nil, http.StatusNotFound, op.Errorf(op.CodeNotFound, "unknown command %q", command)
}

// plan plans a command. A read-only plan is read back at once; any other is
// stored for its apply.
func (s *server) plan(w http.ResponseWriter, r *http.Request) {
	command := r.PathValue("command")
	ia := newInteractor()
	o, status, err := s.build(command, r, ia)
	if err != nil {
		writeError(w, status, err)
		return
	}
	p, err := o.Plan(r.Context())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	if p.ReadOnly {
		res, err := op.Finish(r.Context(), o, p, false)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": p, "result": resultOrEmpty(res)})
		return
	}
	id := rand.Text()
	s.mu.Lock()
	s.plans[id] = &storedPlan{command: command, o: o, p: p, interact: ia}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "plan": p})
}

// apply applies a stored plan once and streams its login steps and its
// final result as NDJSON. A plan refused for want of confirmation stays
// stored.
func (s *server) apply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	sp := s.plans[id]
	if sp == nil {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, op.Errorf(op.CodeNotFound, "no stored plan %q", id))
		return
	}
	if s.applying != "" {
		running := s.applying
		s.mu.Unlock()
		writeError(w, http.StatusConflict, op.Errorf(op.CodeOperationRunning, "%s is being applied; wait for it to finish", running))
		return
	}
	s.applying = sp.command
	sp.busy = true
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	st := &stream{w: w, rc: http.NewResponseController(w)}
	sp.interact.attach(func(step op.LoginStep) { st.line(map[string]any{"event": "login_step", "step": step}) })
	res, err := op.Finish(r.Context(), sp.o, sp.p, body.Confirm)
	sp.interact.attach(nil)

	keep := op.Code(err) == op.CodeConfirmationRequired
	s.mu.Lock()
	s.applying = ""
	sp.busy = false
	if !keep {
		delete(s.plans, id)
	}
	s.mu.Unlock()

	if err != nil {
		st.line(map[string]any{"event": "error", "error": op.AsError(err)})
		return
	}
	st.line(map[string]any{"event": "result", "result": resultOrEmpty(res)})
}

// code hands a pasted code to the login waiting in the plan's apply.
func (s *server) code(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	sp := s.plans[id]
	s.mu.Unlock()
	if sp == nil {
		writeError(w, http.StatusNotFound, op.Errorf(op.CodeNotFound, "no stored plan %q", id))
		return
	}
	if !sp.interact.give(body.Code) {
		writeError(w, http.StatusConflict, op.Errorf(op.CodeInvalidArgument, "no code requested"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// drop deletes a stored plan that is not being applied.
func (s *server) drop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.plans[id]
	if sp == nil {
		writeError(w, http.StatusNotFound, op.Errorf(op.CodeNotFound, "no stored plan %q", id))
		return
	}
	if sp.busy {
		writeError(w, http.StatusConflict, op.Errorf(op.CodeOperationRunning, "%s is being applied", sp.command))
		return
	}
	delete(s.plans, id)
	w.WriteHeader(http.StatusNoContent)
}

// interactor is the Dashboard side of a structured login: steps go to the
// apply stream and the code comes from the /code route.
type interactor struct {
	emitMu sync.Mutex
	emit   func(op.LoginStep)

	mu      sync.Mutex
	waiting bool
	codes   chan string
}

func newInteractor() *interactor { return &interactor{codes: make(chan string, 1)} }

func (i *interactor) attach(emit func(op.LoginStep)) {
	i.emitMu.Lock()
	i.emit = emit
	i.emitMu.Unlock()
}

func (i *interactor) Step(step op.LoginStep) {
	i.emitMu.Lock()
	defer i.emitMu.Unlock()
	if i.emit != nil {
		i.emit(step)
	}
}

// Code waits for the person's code or for ctx to end; there is no timer.
func (i *interactor) Code(ctx context.Context) (string, error) {
	i.mu.Lock()
	i.waiting = true
	i.mu.Unlock()
	select {
	case c := <-i.codes:
		return c, nil
	case <-ctx.Done():
		i.mu.Lock()
		i.waiting = false
		select {
		case <-i.codes:
		default:
		}
		i.mu.Unlock()
		return "", op.Errorf(op.CodeCancelled, "login cancelled: %v", ctx.Err())
	}
}

// give passes code to a waiting Code; it reports false when none waits.
func (i *interactor) give(code string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.waiting {
		return false
	}
	i.waiting = false
	i.codes <- code
	return true
}

// stream writes NDJSON lines, flushing each one.
type stream struct {
	mu sync.Mutex
	w  io.Writer
	rc *http.ResponseController
}

func (st *stream) line(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(map[string]any{"event": "error", "error": op.AsError(err)})
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	_, _ = st.w.Write(append(b, '\n'))
	_ = st.rc.Flush()
}

// decode reads a JSON object strictly into v; an empty body leaves v zero.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return op.Errorf(op.CodeInvalidArgument, "invalid request body: %w", err)
	}
	// Reading to the end also lets the server notice a client that leaves.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return op.Errorf(op.CodeInvalidArgument, "invalid request body: data after the JSON object")
	}
	return nil
}

func resultOrEmpty(r op.Result) op.Result {
	if r == nil {
		return op.Result{}
	}
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		b, _ = json.Marshal(map[string]any{"error": op.AsError(err)})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": op.AsError(err)})
}
