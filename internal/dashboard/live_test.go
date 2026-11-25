package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// fakeWatch sends its scripted events, then waits for ctx or returns err.
type fakeWatch struct {
	mu      sync.Mutex
	events  []any
	err     error
	started chan int
	ended   chan int
	n       int
}

func newFakeWatch(events ...any) *fakeWatch {
	return &fakeWatch{events: events, started: make(chan int, 10), ended: make(chan int, 10)}
}

func (f *fakeWatch) watch(ctx context.Context, send func(any)) error {
	f.mu.Lock()
	f.n++
	n, events, err := f.n, f.events, f.err
	f.mu.Unlock()
	f.started <- n
	defer func() { f.ended <- n }()
	for _, e := range events {
		send(e)
	}
	if err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func wait(t *testing.T, c chan int) int {
	t.Helper()
	select {
	case n := <-c:
		return n
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		return 0
	}
}

// openLive starts GET /api/live and returns the response and a reader of
// its data frames.
func (h *harness) openLive(ctx context.Context) (*http.Response, func() map[string]any) {
	h.t.Helper()
	resp, err := http.DefaultClient.Do(h.request("GET", "/api/live", "", nil).WithContext(ctx))
	if err != nil {
		h.t.Fatal(err)
	}
	rd := bufio.NewReader(resp.Body)
	next := func() map[string]any {
		h.t.Helper()
		var data string
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return nil
			}
			line = strings.TrimSuffix(line, "\n")
			if line == "" {
				break
			}
			d, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				h.t.Fatalf("frame line %q", line)
			}
			data += d
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			h.t.Fatalf("frame %q: %v", data, err)
		}
		return v
	}
	return resp, next
}

func TestLiveNeedsTheSession(t *testing.T) {
	fw := newFakeWatch()
	h := startFull(t, nil, fw.watch)
	status, body, _ := h.do("GET", "/api/live", "", nil)
	if status != http.StatusUnauthorized || errCode(t, body) != op.CodeSessionRequired {
		t.Errorf("no session: %d %s", status, body)
	}
	h.open()
	status, body, _ = h.do("GET", "/api/live?session="+h.session, "", map[string]string{SessionHeader: ""})
	if status != http.StatusUnauthorized {
		t.Errorf("session in the URL: %d %s", status, body)
	}
	if status, body, _ := h.do("GET", "/api/live", "", map[string]string{"Origin": "http://evil.example"}); status != http.StatusForbidden {
		t.Errorf("foreign origin: %d %s", status, body)
	}
	select {
	case <-fw.started:
		t.Error("Watch ran for a refused request")
	default:
	}
}

func TestLiveStreamsFrames(t *testing.T) {
	fw := newFakeWatch(
		map[string]any{"event": "snapshot", "at": "2026-11-25T10:00:00Z", "result": map[string]any{"user": "me"}},
		map[string]any{"event": "checked", "at": "2026-11-25T10:00:30Z"},
	)
	h := startFull(t, nil, fw.watch)
	h.open()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, next := h.openLive(ctx)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("live: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	checkCommonHeaders(t, "live", resp.Header)
	if e := next(); e["event"] != "snapshot" || e["result"].(map[string]any)["user"] != "me" {
		t.Errorf("first = %v", e)
	}
	if e := next(); e["event"] != "checked" || e["at"] != "2026-11-25T10:00:30Z" {
		t.Errorf("second = %v", e)
	}
	// The page leaving ends Watch.
	cancel()
	resp.Body.Close()
	wait(t, fw.ended)
}

func TestLiveSecondStreamEndsTheFirst(t *testing.T) {
	fw := newFakeWatch(map[string]any{"event": "snapshot"})
	h := startFull(t, nil, fw.watch)
	h.open()
	r1, next1 := h.openLive(context.Background())
	defer r1.Body.Close()
	next1()
	if n := wait(t, fw.started); n != 1 {
		t.Fatalf("started %d", n)
	}
	r2, next2 := h.openLive(context.Background())
	defer r2.Body.Close()
	if n := wait(t, fw.ended); n != 1 {
		t.Errorf("ended %d first", n)
	}
	// The first response ends.
	if e := next1(); e != nil {
		t.Errorf("first stream still sends %v", e)
	}
	if e := next2(); e["event"] != "snapshot" {
		t.Errorf("second = %v", e)
	}
	// Ctrl-C ends the second, and Serve returns (checked by the cleanup).
	h.cancel()
	wait(t, fw.ended)
}

func TestLiveWatchErrorEndsWithAnErrorFrame(t *testing.T) {
	fw := newFakeWatch()
	fw.err = op.Errorf(op.CodeAuthUnavailable, "no gh login")
	h := startFull(t, nil, fw.watch)
	h.open()
	resp, next := h.openLive(context.Background())
	defer resp.Body.Close()
	e := next()
	if e["event"] != "error" || e["source"] != "status" || e["error"].(map[string]any)["code"] != op.CodeAuthUnavailable {
		t.Errorf("frame = %v", e)
	}
	if e := next(); e != nil {
		t.Errorf("after the error: %v", e)
	}
}

func TestLiveWithoutWatch(t *testing.T) {
	h := start(t)
	h.open()
	status, body, _ := h.do("GET", "/api/live", "", nil)
	if status != http.StatusNotFound || errCode(t, body) != op.CodeNotFound {
		t.Errorf("live: %d %s", status, body)
	}
}
