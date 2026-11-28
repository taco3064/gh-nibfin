package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/op"
)

// lockedBuffer is safe for the concurrent writes of the dashboard handler.
// When want is set, hit is closed once the written text contains want.
type lockedBuffer struct {
	mu   sync.Mutex
	b    bytes.Buffer
	want string
	hit  chan struct{}
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.b.Write(p)
	if l.want != "" && strings.Contains(l.b.String(), l.want) {
		close(l.hit)
		l.want = ""
	}
	return n, err
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// runDashboard runs gh nibfin dashboard with the real server. Once the
// server is ready it exchanges the proof when exchange is set, waits for the
// browser hook when one is expected, and stops the server.
func runDashboard(t *testing.T, browserErr error, exchange bool, args ...string) (code int, stdout, stderr string, browsed []string) {
	t.Helper()
	fakeRepo(t)
	var mu sync.Mutex
	browserDone := make(chan struct{}, 1)
	oldOpen, oldServe := openBrowser, dashboardServe
	t.Cleanup(func() { openBrowser, dashboardServe = oldOpen, oldServe })
	openBrowser = func(url string, _, _ io.Writer) error {
		mu.Lock()
		browsed = append(browsed, url)
		mu.Unlock()
		defer func() { browserDone <- struct{}{} }()
		return browserErr
	}
	// The command prints a browser failure after openBrowser returns, so the
	// server is stopped only once that line is out.
	var out, errOut lockedBuffer
	if browserErr != nil {
		out.want, out.hit = "cannot open a browser: ", make(chan struct{})
	}
	noBrowser := false
	for _, a := range args {
		noBrowser = noBrowser || a == "--no-browser"
	}
	dashboardServe = func(ctx context.Context, o dashboard.Options) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		ready := o.Ready
		o.Ready = func(url string) {
			ready(url)
			go func() {
				defer cancel()
				if !noBrowser {
					<-browserDone
					if out.hit != nil {
						<-out.hit
					}
				}
				if exchange {
					proof := url[strings.Index(url, "#proof=")+len("#proof="):]
					req, _ := http.NewRequest(http.MethodPost, strings.SplitN(url, "#", 2)[0]+"api/session", nil)
					req.Header.Set(dashboard.ProofHeader, proof)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Error(err)
						return
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						t.Errorf("exchange status %d", resp.StatusCode)
					}
				}
			}()
		}
		return oldServe(ctx, o)
	}
	code = Execute(append([]string{"dashboard"}, args...), strings.NewReader(""), &out, &errOut)
	mu.Lock()
	defer mu.Unlock()
	return code, out.String(), errOut.String(), browsed
}

func TestDashboardNoBrowserPrintsTheSessionURL(t *testing.T) {
	code, stdout, stderr, browsed := runDashboard(t, nil, true, "--no-browser")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if len(browsed) != 0 {
		t.Errorf("a browser was opened: %v", browsed)
	}
	if !strings.Contains(stdout, "http://127.0.0.1:") || !strings.Contains(stdout, "/#proof=") {
		t.Errorf("stdout has no session URL: %q", stdout)
	}
	if !strings.HasSuffix(stdout, "session opened\n") {
		t.Errorf("stdout does not report the exchange: %q", stdout)
	}
}

func TestDashboardOpensTheBrowser(t *testing.T) {
	code, stdout, _, browsed := runDashboard(t, nil, false)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(browsed) != 1 || !strings.HasPrefix(browsed[0], "http://127.0.0.1:") || !strings.Contains(browsed[0], "/#proof=") {
		t.Fatalf("browser got %v", browsed)
	}
	if strings.Contains(stdout, "#proof=") || !strings.HasPrefix(stdout, "Dashboard: http://127.0.0.1:") || !strings.Contains(stdout, "opening your browser") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestDashboardBrowserFailurePrintsTheSessionURL(t *testing.T) {
	code, stdout, _, browsed := runDashboard(t, errors.New("no launcher"), false)
	if code != 0 || len(browsed) != 1 {
		t.Fatalf("exit %d, browsed %v", code, browsed)
	}
	if !strings.Contains(stdout, "cannot open a browser: no launcher") || !strings.Contains(stdout, browsed[0]) {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestDashboardBadPort(t *testing.T) {
	code, stdout, stderr, _ := runDashboard(t, nil, false, "--port", "70000", "--no-browser")
	if code == 0 || !strings.HasPrefix(stderr, op.CodeInvalidArgument+": ") || stdout != "" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestDashboardWiresLiveUpdates(t *testing.T) {
	old := dashboardServe
	t.Cleanup(func() { dashboardServe = old })
	var got dashboard.Options
	dashboardServe = func(_ context.Context, o dashboard.Options) error { got = o; return nil }
	var out, errOut lockedBuffer
	if code := Execute([]string{"dashboard", "--no-browser"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got.Watch == nil {
		t.Error("the Dashboard has no live updates")
	}
}
