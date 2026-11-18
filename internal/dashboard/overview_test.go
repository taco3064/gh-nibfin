package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type overviewFixture struct {
	started chan struct{}
	release chan struct{}
	value   snapshot.Overview
}

func (f overviewFixture) CollectOverview() snapshot.Overview {
	select {
	case <-f.started:
	default:
		close(f.started)
	}
	<-f.release
	return f.value
}

func overviewValue() snapshot.Overview {
	status := snapshot.Status{Availability: snapshot.Known, Complete: true}
	return snapshot.Overview{SchemaVersion: 1, ObservedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Minute), Subject: snapshot.Subject{ID: 7,
			Login: "reader"}, Discovery: status,
		Repositories: snapshot.Section[snapshot.Repository]{Status: status,
			Items: []snapshot.Repository{{ID: 1, Name: "example/project"}}},
		Issues: snapshot.Section[snapshot.OverviewIssue]{Status: status,
			Items: []snapshot.OverviewIssue{{Key: "1:I_one"}}},
		Runners: snapshot.Section[snapshot.LocalRunner]{Status: status,
			Items: []snapshot.LocalRunner{{Key: "1:2"}}}}
}

func overviewGET(t *testing.T, s *Service, path, token, cursor string) *http.Response {
	t.Helper()
	r, _ := http.NewRequest("GET", s.URL()+path, nil)
	if token != "" {
		r.Header.Set(SessionHeader, token)
	}
	if cursor != "" {
		r.Header.Set("Last-Event-ID", cursor)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestOverviewStreamAuthenticationAndIndependentCollection(t *testing.T) {
	fixture := overviewFixture{make(chan struct{}), make(chan struct{}), overviewValue()}
	s := launch(t, Config{Overview: fixture, Provider: &recordingCollector{
		collect: func(string) snapshot.Snapshot { return projectSnapshot() },
	}})
	allowProject(s)
	<-fixture.started
	token := strings.Split(proof(s), ":")[0]
	for _, path := range []string{"/api/overview", "/api/updates"} {
		response := overviewGET(t, s, path, "", "")
		response.Body.Close()
		if response.StatusCode != 401 || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("unauthenticated read", response.StatusCode)
		}
	}
	// Overview is deliberately blocked; the existing detail collector still runs.
	if code, _ := request(t, s, token); code != 200 {
		t.Fatal("overview froze repository detail", code)
	}
	close(fixture.release)
	response := overviewGET(t, s, "/api/updates", token, "")
	reader := bufio.NewReader(response.Body)
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		frame.WriteString(line)
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(frame.String(), "event: reset") ||
		!strings.Contains(frame.String(), "1:I_one") {
		t.Fatal("missing initial reset", frame.String())
	}
	if status, _ := request(t, s, token); status != 200 {
		t.Fatal("stream occupied collection slot", status)
	}
	// Drain while shutdown runs: unrelated connections can delay Shutdown beyond
	// the stream client's timeout even after this handler has already returned.
	drained := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(reader)
		response.Body.Close()
		drained <- err
	}()
	// Shutdown cancels a live stream without waiting for its lifetime timer.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-drained; err != nil {
		t.Fatal("stream did not close cleanly", err)
	}
}

func TestOverviewStreamClosesBeforePendingDetailCollection(t *testing.T) {
	fixture := overviewFixture{make(chan struct{}), make(chan struct{}), overviewValue()}
	close(fixture.release)
	started, release := make(chan struct{}), make(chan struct{})
	provider := &recordingCollector{collect: func(string) snapshot.Snapshot {
		close(started)
		<-release
		return snapshot.Snapshot{}
	}}
	s := launch(t, Config{Overview: fixture, Provider: provider})
	allowProject(s)
	// Release the synchronous detail collector before launch's cleanup on failure.
	defer close(release)
	token := strings.Split(proof(s), ":")[0]
	stream := overviewGET(t, s, "/api/updates", token, "")
	defer stream.Body.Close()
	detailDone := make(chan error, 1)
	go func() {
		r, _ := http.NewRequest("GET", s.URL()+"/api/snapshot", nil)
		r.Header.Set(SessionHeader, token)
		response, err := http.DefaultClient.Do(r)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		detailDone <- err
	}()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	// The stream must end cleanly while shutdown still waits for detail work.
	if _, err := io.Copy(io.Discard, stream.Body); err != nil {
		t.Fatal("pending detail delayed stream termination", err)
	}
	select {
	case err := <-closed:
		t.Fatal("shutdown did not wait for detail collection", err)
	default:
	}
	// The deferred release unblocks collection; cleanup waits for shutdown.
	t.Cleanup(func() {
		if err := <-detailDone; err != nil {
			t.Error("detail response did not close cleanly", err)
		}
		if err := <-closed; err != nil {
			t.Error(err)
		}
	})
}

func TestOverviewReplayAndPartialReconciliation(t *testing.T) {
	history := []overviewEvent{{id: 3}, {id: 4}, {id: 5}}
	for _, test := range []struct {
		last uint64
		want []uint64
	}{{0, []uint64{5}}, {1, []uint64{5}}, {3, []uint64{4, 5}},
		{5, nil}, {100, []uint64{5}}} {
		got := overviewEvents(history, test.last)
		if len(got) != len(test.want) {
			t.Fatal("wrong replay", test.last, got)
		}
		for i, event := range got {
			if event.id != test.want[i] {
				t.Fatal("unordered replay")
			}
		}
	}
	old := overviewValue()
	next := snapshot.Overview{}
	partial := reconcileOverview(old, next)
	if len(partial.Issues.Items) != 1 || len(partial.Runners.Items) != 1 ||
		!partial.Issues.Items[0].Retained || !partial.Runners.Items[0].Retained ||
		!partial.Repositories.Items[0].Retained || old.Repositories.Items[0].Retained ||
		old.Issues.Items[0].Retained || old.Runners.Items[0].Retained {
		t.Fatal("partial absence removed rows")
	}
	next.Issues.Status.Complete, next.Runners.Status.Complete = true, true
	complete := reconcileOverview(old, next)
	if len(complete.Issues.Items) != 0 || len(complete.Runners.Items) != 0 {
		t.Fatal("complete removal did not converge")
	}
}

func TestOverviewRepositorySelection(t *testing.T) {
	fixture := overviewFixture{make(chan struct{}), make(chan struct{}), overviewValue()}
	close(fixture.release)
	s := launch(t, Config{Overview: fixture})
	token := strings.Split(proof(s), ":")[0]
	response := overviewGET(t, s, "/api/updates", token, "")
	response.Body.Close() // Wait until the first permission-checked publication.
	for _, name := range []string{"other/project", "example/project"} {
		body, _ := json.Marshal(map[string]string{"repository": name})
		r, _ := http.NewRequestWithContext(context.Background(), "POST",
			s.URL()+"/api/repository", strings.NewReader(string(body)))
		r.Header.Set(SessionHeader, token)
		r.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := 200
		if name == "other/project" {
			want = 403
		}
		if response.StatusCode != want {
			t.Fatal("repository allowlist failure", name, response.StatusCode)
		}
	}
}

func TestOverviewOversizedProjectionKeepsRowsAndReportsLimit(t *testing.T) {
	old := overviewValue()
	next := overviewValue()
	next.Issues.Items[0].Title = strings.Repeat("x", overviewByteLimit)
	value, data := encodeOverview(old, next, 2)
	if data == nil || len(data) > overviewByteLimit || value.Issues.Status.Complete ||
		value.Issues.Status.Reason != snapshot.Limit || len(value.Issues.Items) != 1 ||
		value.Issues.Items[0].Title != old.Issues.Items[0].Title {
		t.Fatal("oversized projection silently dropped state")
	}
}

func TestOverviewExpiryTerminatesStreamAndRefusesReads(t *testing.T) {
	fixture := overviewFixture{make(chan struct{}), make(chan struct{}), overviewValue()}
	close(fixture.release)
	base := time.Now()
	var offset atomic.Int64
	s := launch(t, Config{Overview: fixture, Now: func() time.Time {
		return base.Add(time.Duration(offset.Load()))
	}})
	token := strings.Split(proof(s), ":")[0]
	response := overviewGET(t, s, "/api/updates", token, "")
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\n" {
			break
		}
	}
	offset.Store(int64(SessionLifetime + time.Second))
	s.mu.Lock()
	close(s.overview.changed)
	s.overview.changed = make(chan struct{})
	s.mu.Unlock()
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal("expired stream did not terminate", err)
	}
	response.Body.Close()
	denied := overviewGET(t, s, "/api/overview", token, "")
	denied.Body.Close()
	if denied.StatusCode != 401 {
		t.Fatal("expired read disclosed overview", denied.StatusCode)
	}
}

func projectSnapshot() snapshot.Snapshot {
	return snapshot.Snapshot{SchemaVersion: 1, StartedAt: time.Now(),
		ExpiresAt:  time.Now().Add(time.Minute),
		Repository: overviewValue().Repositories,
		Issues: snapshot.Section[snapshot.Issue]{Status: snapshot.Status{
			Availability: snapshot.Known, Complete: true}, Items: []snapshot.Issue{}}}
}

func allowProject(s *Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overview.current = overviewValue()
	s.selected = s.overview.current.Repositories.Items[0]
}
