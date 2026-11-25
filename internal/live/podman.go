package live

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/taco3064/gh-nibfin/internal/host"
	"github.com/taco3064/gh-nibfin/internal/op"
)

// The labels the Job template puts on each Job container.
const (
	repoLabel  = "io.github.taco3064.nibfin.repo"
	issueLabel = "io.github.taco3064.nibfin.issue"
)

// podmanEvents starts podman events for Job containers on the Runners'
// store and returns its output and its wait. It never starts a machine.
// Tests replace it.
var podmanEvents = func(ctx context.Context) (io.Reader, func() error, error) {
	h, err := host.Detect(ctx)
	if err != nil {
		return nil, nil, hostErr(err)
	}
	if !h.Running {
		return nil, nil, op.Errorf(op.CodeMachineStopped, "%s is stopped; the Dashboard does not start it", h.Describe())
	}
	// One filter: how several combine is not documented.
	c := h.Podman(ctx, "events", "--format", "json", "--filter", "label="+repoLabel)
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, nil, op.Errorf(op.CodePodmanFailed, "podman events: %v", err)
	}
	stderr := &tail{}
	c.Stderr = stderr
	if err := c.Start(); err != nil {
		return nil, nil, op.Errorf(op.CodePodmanFailed, "podman events: %v", err)
	}
	wait := func() error {
		err := c.Wait()
		msg := strings.TrimSpace(stderr.String())
		switch {
		case err != nil && msg != "":
			return op.Errorf(op.CodePodmanFailed, "podman events stopped: %v: %s", err, msg)
		case err != nil:
			return op.Errorf(op.CodePodmanFailed, "podman events stopped: %v", err)
		}
		return op.Errorf(op.CodePodmanFailed, "podman events stopped")
	}
	return out, wait, nil
}

func hostErr(err error) error {
	for _, m := range []struct {
		target error
		code   string
	}{
		{host.ErrPodmanMissing, op.CodePodmanMissing},
		{host.ErrMachineMissing, op.CodeMachineMissing},
		{host.ErrUnsupported, op.CodeUnsupportedHost},
	} {
		if errors.Is(err, m.target) {
			return op.Wrap(m.code, err)
		}
	}
	return op.Wrap(op.CodePodmanFailed, err)
}

// tail keeps the last 4 KiB written to it.
type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = t.b[len(t.b)-4096:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(bytes.ToValidUTF8(t.b, nil))
}

// startPodman starts podman events and follows them in the background
// until they end or ctx does; the end arrives on podDone. A failed start is
// sent at once and tried again on the next tick.
func (w *watcher) startPodman(ctx context.Context, wg *sync.WaitGroup) {
	out, wait, err := podmanEvents(ctx)
	if err != nil {
		w.podman(false, err)
		return
	}
	w.podOn = true
	w.podman(true, nil)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := w.follow(out, wait)
		select {
		case w.podDone <- err:
		case <-ctx.Done():
		}
	}()
}

func (w *watcher) follow(out io.Reader, wait func() error) error {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		w.event(sc.Bytes())
	}
	// Drain what a too-long line left, so the process can exit.
	_, _ = io.Copy(io.Discard, out)
	return wait()
}

// podman sends the follower's state when it changed.
func (w *watcher) podman(watching bool, err error) {
	e := op.AsError(err)
	state := strconv.FormatBool(watching)
	if e != nil {
		state += " " + e.Code + " " + e.Error()
	}
	w.mu.Lock()
	same := state == w.podState
	w.podState = state
	w.mu.Unlock()
	if !same {
		w.send(Event{Event: "podman", Watching: &watching, Error: e})
	}
}

// event marks the listed Issue whose Job container started or ended. The
// event's labels only select an Issue already listed; they never build a
// request.
func (w *watcher) event(line []byte) {
	var e struct {
		Type       string            `json:"Type"`
		Status     string            `json:"Status"`
		Attributes map[string]string `json:"Attributes"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "container" || (e.Status != "start" && e.Status != "died") {
		return
	}
	repo, okRepo := e.Attributes[repoLabel]
	issue, okIssue := e.Attributes[issueLabel]
	if !okRepo && !okIssue {
		// The filter matched but the labels are not in the attributes.
		w.mark(true)
		return
	}
	n, err := strconv.Atoi(issue)
	if err != nil || n <= 0 || strconv.Itoa(n) != issue || repo == "" {
		return
	}
	k := key(repo, n)
	w.mu.Lock()
	_, listed := w.v.listed[k]
	elsewhere := w.v.execElsewhere
	w.mu.Unlock()
	// The Runners serve the execution repository, so their Jobs' Issues are
	// on its host.
	if listed && !elsewhere {
		w.mark(false, k)
	}
}
