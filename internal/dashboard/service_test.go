package dashboard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

type recordingCollector struct {
	calls   atomic.Int32
	collect func(string) snapshot.Snapshot
}

func (p *recordingCollector) Collect(name string) snapshot.Snapshot {
	p.calls.Add(1)
	if p.collect != nil {
		return p.collect(name)
	}
	return (snapshot.Provider{}).Collect(name)
}

func launch(t *testing.T, c Config) *Service {
	t.Helper()
	if c.Repository == "" {
		c.Repository = "example/project"
	}
	if c.Provider == nil {
		c.Provider = &recordingCollector{}
	}
	s, err := Start(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func proof(s *Service) string {
	return strings.TrimPrefix(s.BootstrapURL(), s.URL()+"/#")
}

func request(t *testing.T, s *Service, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", s.URL()+"/api/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set(SessionHeader, token)
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(data)
}

func TestRealLifecycleIsolationAndRepeatedShutdown(t *testing.T) {
	a := &recordingCollector{}
	b := &recordingCollector{}
	a.collect = func(name string) snapshot.Snapshot {
		if name != "first/repo" {
			t.Error("wrong first repository", name)
		}
		return (snapshot.Provider{}).Collect(name)
	}
	b.collect = func(name string) snapshot.Snapshot {
		if name != "second/repo" {
			t.Error("wrong second repository", name)
		}
		return (snapshot.Provider{}).Collect(name)
	}
	first := launch(t, Config{Repository: "first/repo", Provider: a})
	second := launch(t, Config{Repository: "second/repo", Provider: b})
	if first.URL() == second.URL() || proof(first) == proof(second) {
		t.Fatal("instances share authority or proof")
	}
	for _, s := range []*Service{first, second} {
		if code, _ := request(t, s, proof(s)); code != 200 {
			t.Fatal(code)
		}
	}
	if code, _ := request(t, second, proof(first)); code != 401 {
		t.Fatal(code)
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatal("invalid proof collected")
	}
	token := proof(first)
	for i := 0; i < 3; i++ {
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if first.BootstrapURL() != "" {
		t.Fatal("closed session available")
	}
	if code := first.acquire(&http.Request{Header: http.Header{
		SessionHeader: []string{token}}}); code != 503 {
		t.Fatal(code)
	}
	u, _ := url.Parse(first.URL())
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("listener leaked")
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) {
	return 0, errors.New("private failure details")
}

func TestStartupFailuresBeforeListenerAndPortReuse(t *testing.T) {
	bad := []Config{
		{Repository: "bad"},
		{Repository: "example/project", Bind: "0.0.0.0:0"},
		{Repository: "example/project", Bind: "[::]:0"},
		{Repository: "example/project", Bind: "localhost:0"},
		{Repository: "example/project", Bind: "192.168.1.1:0"},
		{Repository: "example/project", Bind: "127.0.0.1:000"},
		{Repository: "example/project", Bind: "127.0.0.1:65536"},
		{Repository: "example/project", Random: brokenReader{}},
		{Repository: "example/project", Assets: map[string]Asset{}},
	}
	for _, c := range bad {
		c.Provider = &recordingCollector{}
		called := false
		_, err := start(context.Background(), c,
			func(_, _ string) (net.Listener, error) { called = true; return nil, nil })
		if err == nil || called {
			t.Fatalf("unsafe startup: %+v", c)
		}
	}
	c := Config{Repository: "example/project", Provider: &recordingCollector{}}
	_, err := start(context.Background(), c, func(_, _ string) (net.Listener, error) {
		return nil, errors.New("/private/socket token")
	})
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, c); err == nil {
		t.Fatal("canceled startup")
	}
	s := launch(t, c)
	authority := strings.TrimPrefix(s.URL(), "http://")
	c.Bind = authority
	if _, err := Start(context.Background(), c); err == nil {
		t.Fatal("port collision")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := launch(t, c)
	if replacement.URL() != s.URL() {
		t.Fatal("listener not released")
	}
}

func TestSessionExpiryAndCancellation(t *testing.T) {
	var clock atomic.Int64
	base := time.Now()
	now := func() time.Time { return base.Add(time.Duration(clock.Load())) }
	collector := &recordingCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Start(ctx, Config{Repository: "example/project", Provider: collector,
		Now: now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	token := proof(s)
	clock.Store(int64(SessionLifetime - time.Nanosecond))
	if code, _ := request(t, s, token); code != 200 {
		t.Fatal(code)
	}
	clock.Store(int64(SessionLifetime))
	if code, _ := request(t, s, token); code != 401 {
		t.Fatal(code)
	}
	if collector.calls.Load() != 1 || s.BootstrapURL() != "" {
		t.Fatal("expired session used")
	}
	cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop service")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrencyAndInflightShutdown(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	collector := &recordingCollector{collect: func(name string) snapshot.Snapshot {
		close(entered)
		<-release
		return (snapshot.Provider{}).Collect(name)
	}}
	s := launch(t, Config{Provider: collector})
	token := proof(s)
	response := make(chan int, 1)
	go func() { code, _ := request(t, s, token); response <- code }()
	<-entered
	if code, _ := request(t, s, token); code != 503 {
		t.Fatal(code)
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-closed:
		t.Fatal("shutdown abandoned active collection")
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown failed to join collection")
	}
	if code := <-response; code != 401 {
		t.Fatal("shutdown disclosed data", code)
	}
	if collector.calls.Load() != 1 {
		t.Fatal("concurrent collections exceeded bound")
	}
}

func TestConcurrentCloseAndExpiryDuringCollection(t *testing.T) {
	var elapsed atomic.Int64
	base := time.Now()
	now := func() time.Time { return base.Add(time.Duration(elapsed.Load())) }
	collector := &recordingCollector{collect: func(name string) snapshot.Snapshot {
		elapsed.Store(int64(SessionLifetime))
		return (snapshot.Provider{}).Collect(name)
	}}
	s := launch(t, Config{Provider: collector, Now: now})
	if code, _ := request(t, s, proof(s)); code != 401 {
		t.Fatal("expired result disclosed", code)
	}
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
}
