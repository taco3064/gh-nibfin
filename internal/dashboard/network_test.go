package dashboard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type failedListener struct{ net.Listener }

func (l failedListener) Accept() (net.Conn, error) {
	return nil, errors.New("private listener diagnostic")
}

func TestServingFailureReleasesListener(t *testing.T) {
	c := Config{Repository: "example/project", Provider: &recordingCollector{}}
	listen := func(network, address string) (net.Listener, error) {
		l, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		return failedListener{l}, nil
	}
	s, err := start(context.Background(), c, listen)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("serving failure left workers running")
	}
	if err := s.Close(); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("missing or unsafe serving failure", err)
	}
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(s.URL(), "http://"), time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("failed listener leaked")
	}
	if s.BootstrapURL() != "" {
		t.Fatal("failed service kept session")
	}
}

func TestWireHeaderBudgetAndAuthority(t *testing.T) {
	p := &recordingCollector{}
	s := launch(t, Config{Provider: p})
	address := strings.TrimPrefix(s.URL(), "http://")
	for _, tc := range []struct {
		host, extra string
		want        int
	}{
		{"evil.example", "", 403},
		{address, "X-Large: " + strings.Repeat("x", 16*1024) + "\r\n", 431},
		{address, "Origin: null\r\n", 403},
		{address, "Sec-Fetch-Site: cross-site\r\n", 403},
		{address, "", 401},
	} {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, err = fmt.Fprintf(conn, "GET /api/snapshot HTTP/1.1\r\nHost: %s\r\n"+
			"%sConnection: close\r\n\r\n", tc.host, tc.extra)
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		conn.Close()
		if response.StatusCode != tc.want {
			t.Fatal(response.StatusCode, tc.want)
		}
	}
	if p.calls.Load() != 0 {
		t.Fatal("wire rejection collected")
	}
	if s.server.ReadHeaderTimeout <= 0 || s.server.ReadTimeout <= 0 ||
		s.server.WriteTimeout != RequestBound || s.server.IdleTimeout <= 0 ||
		ShutdownBound <= CollectionBound {
		t.Fatal("missing finite network bounds")
	}
}

func TestIPv6LiteralLoopback(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable:", err)
	}
	listener.Close()
	s := launch(t, Config{Bind: "[::1]:0"})
	if !strings.HasPrefix(s.URL(), "http://[::1]:") {
		t.Fatal("not IPv6 loopback")
	}
	if code, _ := request(t, s, proof(s)); code != 200 {
		t.Fatal(code)
	}
}
