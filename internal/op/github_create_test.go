package op

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
)

// createServer answers each create with 502 for the first fails sends;
// with landed the first send still takes effect, as when only the answer
// is lost.
type createServer struct {
	mu      sync.Mutex
	fails   int
	landed  bool
	sends   int
	created []string
	since   []string
}

func (s *createServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodGet {
		s.since = append(s.since, r.URL.Query().Get("since"))
		io.WriteString(w, "["+strings.Join(s.created, ",")+"]")
		return
	}
	s.sends++
	b, _ := io.ReadAll(r.Body)
	var one string
	if strings.HasSuffix(r.URL.Path, "/comments") {
		// GitHub stores a comment's CRLF as LF.
		one = strings.ReplaceAll(string(b), `\r\n`, `\n`)
		one = strings.TrimSuffix(one, "}") + fmt.Sprintf(`,"id":%d}`, 40+len(s.created))
	} else {
		one = strings.TrimSuffix(string(b), "}") + `,"number":7,"html_url":"https://x/7"}`
	}
	if s.sends <= s.fails {
		if s.landed && s.sends == 1 {
			s.created = append(s.created, one)
		}
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"message":"Bad Gateway"}`)
		return
	}
	s.created = append(s.created, one)
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, one)
}

func createClient(t *testing.T, s *createServer) restGitHub {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	c, err := api.NewRESTClient(api.ClientOptions{Host: "ghe.example", AuthToken: "x", Transport: ghretry.Transport(rewrite{u})})
	if err != nil {
		t.Fatal(err)
	}
	oldSleep, oldLog, oldNow := ghretry.Sleep, ghretry.Log, now
	ghretry.Sleep = func(context.Context, time.Duration) error { return nil }
	ghretry.Log = io.Discard
	now = func() time.Time { return time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { ghretry.Sleep, ghretry.Log, now = oldSleep, oldLog, oldNow })
	return restGitHub{c}
}

func TestCreateIssueRetriesWithoutDuplicates(t *testing.T) {
	ctx := context.Background()
	// The first send fails before it lands: sent again, one Issue.
	s := &createServer{fails: 1}
	is, err := createClient(t, s).CreateIssue(ctx, "ghe.example/o/r", "T", "B")
	if err != nil || is.Number != 7 || s.sends != 2 || len(s.created) != 1 {
		t.Fatalf("lost send: %+v %v, %d sends, %d created", is, err, s.sends, len(s.created))
	}
	if s.since[0] != "2026-11-26T13:59:00Z" {
		t.Fatalf("since %q", s.since[0])
	}
	// The first send lands but its answer is lost: not sent again.
	s = &createServer{fails: 1, landed: true}
	is, err = createClient(t, s).CreateIssue(ctx, "ghe.example/o/r", "T", "B")
	if err != nil || is.Number != 7 || s.sends != 1 || len(s.created) != 1 {
		t.Fatalf("lost answer: %+v %v, %d sends, %d created", is, err, s.sends, len(s.created))
	}
	// Every send fails: 4 sends, the last error.
	s = &createServer{fails: 9}
	if _, err := createClient(t, s).CreateIssue(ctx, "ghe.example/o/r", "T", "B"); Code(err) != CodeGitHubAPIFailed || s.sends != 4 {
		t.Fatalf("all fail: %v after %d sends", err, s.sends)
	}
}

func TestCreateCommentRetriesWithoutDuplicates(t *testing.T) {
	ctx := context.Background()
	s := &createServer{fails: 2}
	if err := createClient(t, s).CreateComment(ctx, "ghe.example/o/r", 7, "hi"); err != nil || s.sends != 3 || len(s.created) != 1 {
		t.Fatalf("lost sends: %v, %d sends, %d created", err, s.sends, len(s.created))
	}
	s = &createServer{fails: 1, landed: true}
	if err := createClient(t, s).CreateComment(ctx, "ghe.example/o/r", 7, "hi"); err != nil || s.sends != 1 || len(s.created) != 1 {
		t.Fatalf("lost answer: %v, %d sends, %d created", err, s.sends, len(s.created))
	}
	// GitHub may store CRLF as LF: still the same comment.
	s = &createServer{fails: 1, landed: true}
	if err := createClient(t, s).CreateComment(ctx, "ghe.example/o/r", 7, "a\r\nb"); err != nil || s.sends != 1 {
		t.Fatalf("CRLF: %v, %d sends", err, s.sends)
	}
	s = &createServer{fails: 9}
	if err := createClient(t, s).CreateComment(ctx, "ghe.example/o/r", 7, "hi"); Code(err) != CodeGitHubAPIFailed || s.sends != 4 {
		t.Fatalf("all fail: %v after %d sends", err, s.sends)
	}
}

func TestCreateCommentID(t *testing.T) {
	ctx := context.Background()
	s := &createServer{fails: 1}
	if id, err := createClient(t, s).CreateCommentID(ctx, "ghe.example/o/r", 7, "hi"); err != nil || id != 40 || s.sends != 2 {
		t.Fatalf("lost send: %d %v, %d sends", id, err, s.sends)
	}
	// The answer is lost: the id is the re-read comment's.
	s = &createServer{fails: 1, landed: true}
	s.created = []string{`{"body":"other","id":3}`}
	if id, err := createClient(t, s).CreateCommentID(ctx, "ghe.example/o/r", 7, "hi"); err != nil || id != 41 || s.sends != 1 {
		t.Fatalf("lost answer: %d %v, %d sends", id, err, s.sends)
	}
}
