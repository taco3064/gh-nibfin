// Package dashboard hosts a session-scoped, read-only loopback snapshot service.
package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

const (
	SessionLifetime = 30 * time.Minute
	// CollectionBound includes the provider deadline and its final transport call.
	CollectionBound = 4 * time.Minute
	RequestBound    = CollectionBound + 5*time.Second
	ShutdownBound   = RequestBound + 5*time.Second
	SessionHeader   = "X-Nibfin-Session"
)

// Collector implementations must return within CollectionBound. The native
// snapshot.Provider with cli.GitHub/cli.NativeProcess satisfies this contract.
// Implementations are trusted server configuration, never supplied by a browser.
type Collector interface {
	Collect(string) snapshot.Snapshot
}

// Config is trusted local configuration. Assets are copied at startup. A nil map
// selects the embedded shell. Now and Random are injectable test seams; production
// should leave them nil to use time.Now and crypto/rand.Reader.
type Config struct {
	Repository string
	Bind       string
	Provider   Collector
	Assets     map[string]Asset
	Now        func() time.Time
	Random     io.Reader
}

type Service struct {
	server     *http.Server
	listener   net.Listener
	origin     string
	repository string
	provider   Collector
	assets     map[string]Asset
	now        func() time.Time
	expires    time.Time
	secret     string
	mu         sync.Mutex
	closed     bool
	busy       bool
	stop       chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
	closeErr   error
}

// Start binds a literal loopback address, selecting an available port by default.
// The caller owns Close, including after context cancellation. No CLI is invoked.
func Start(ctx context.Context, config Config) (*Service, error) {
	return start(ctx, config, net.Listen)
}

func start(ctx context.Context, c Config,
	listen func(string, string) (net.Listener, error)) (*Service, error) {
	if ctx == nil || !snapshot.ValidRepository(c.Repository) || c.Provider == nil {
		return nil, errors.New("invalid dashboard configuration")
	}
	address, err := bindAddress(c.Bind)
	if err != nil {
		return nil, err
	}
	assets, err := copyAssets(c.Assets)
	if err != nil {
		return nil, err
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Random == nil {
		c.Random = rand.Reader
	}
	var random [32]byte
	if _, err := io.ReadFull(c.Random, random[:]); err != nil {
		return nil, errors.New("dashboard session unavailable")
	}
	if ctx.Err() != nil {
		return nil, errors.New("dashboard start canceled")
	}
	listener, err := listen("tcp", address)
	if err != nil {
		return nil, errors.New("dashboard listener unavailable")
	}
	s := &Service{listener: listener, origin: "http://" + listener.Addr().String(),
		repository: c.Repository, provider: c.Provider, assets: assets, now: c.Now,
		expires: c.Now().Add(SessionLifetime), secret: hex.EncodeToString(random[:]),
		stop: make(chan struct{}), done: make(chan struct{})}
	s.server = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: RequestBound,
		IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 * 1024,
		ErrorLog: log.New(io.Discard, "", 0)}
	served := make(chan error, 1)
	go func() { served <- s.server.Serve(listener) }()
	go s.lifecycle(ctx, served)
	return s, nil
}

func bindAddress(address string) (string, error) {
	if address == "" {
		address = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	number, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil ||
		number < 0 || number > 65535 || strconv.Itoa(number) != port {
		return "", errors.New("literal loopback address required")
	}
	return net.JoinHostPort(ip.String(), port), nil
}

func (s *Service) lifecycle(ctx context.Context, served <-chan error) {
	var serveErr error
	servingStopped := false
	select {
	case <-ctx.Done():
	case <-s.stop:
	case serveErr = <-served:
		servingStopped = true
	}
	s.mu.Lock()
	s.closed, s.secret = true, ""
	s.mu.Unlock()
	shutdown, cancel := context.WithTimeout(context.Background(), ShutdownBound)
	defer cancel()
	if err := s.server.Shutdown(shutdown); err != nil {
		s.closeErr = errors.New("dashboard shutdown bound exceeded")
		_ = s.server.Close()
	}
	if !servingStopped {
		serveErr = <-served
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		s.closeErr = errors.New("dashboard serving failed")
	}
	close(s.done)
}

// URL is safe to display as the service authority; it contains no credentials.
func (s *Service) URL() string { return s.origin }

// BootstrapURL is sensitive local handoff data. Open it deliberately in a browser;
// never log it, persist it, or pass it through a remote website or redirector.
func (s *Service) BootstrapURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.now().Before(s.expires) {
		return ""
	}
	return s.origin + "/#" + s.secret
}

// Close is concurrent-safe and idempotent. It invalidates proof before returning
// and waits for active handlers (up to ShutdownBound), not detached collectors.
func (s *Service) Close() error {
	s.mu.Lock()
	s.closed, s.secret = true, ""
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	return s.closeErr
}
