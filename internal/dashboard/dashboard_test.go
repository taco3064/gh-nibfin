package dashboard

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// fakeOp records its calls; apply, when set, runs as its Apply.
type fakeOp struct {
	mu    sync.Mutex
	plan  op.Plan
	err   error
	apply func(ctx context.Context) error
	calls []string
}

func (f *fakeOp) record(c string) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
}

func (f *fakeOp) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeOp) Plan(context.Context) (op.Plan, error) { f.record("plan"); return f.plan, f.err }
func (f *fakeOp) Apply(ctx context.Context, _ op.Plan) error {
	f.record("apply")
	if f.apply != nil {
		return f.apply(ctx)
	}
	return nil
}
func (f *fakeOp) ReadBack(context.Context, op.Plan) (op.Result, error) {
	f.record("readback")
	return op.Result{"operation": f.plan.Operation}, nil
}

// harness is a running server with fake Ops.
type harness struct {
	t       *testing.T
	base    string
	host    string
	proof   string
	session string
	opened  int
	mu      sync.Mutex
	params  []any
	next    func() *fakeOp // the operation the next entry returns
	gap     chan struct{}  // passed to the next fake login
	cancel  context.CancelFunc
	done    chan error
}

func start(t *testing.T) *harness { t.Helper(); return startWith(t, nil) }

// startWith starts the server with commands as its schema.
func startWith(t *testing.T, commands []Command) *harness {
	t.Helper()
	h := &harness{t: t, done: make(chan error, 1)}
	h.next = func() *fakeOp { return &fakeOp{plan: op.Plan{Operation: "x"}} }
	entry := func(p any) op.Operation {
		h.mu.Lock()
		h.params = append(h.params, p)
		h.mu.Unlock()
		return h.next()
	}
	ops := Ops{
		RunnerAdd: func(p op.RunnerAddParams) op.Operation {
			if p.Interact == nil || p.Terminal != nil {
				t.Errorf("runner add params = %+v", p)
			}
			if o := h.next(); o.apply != nil {
				return &loginOp{fakeOp: o, ia: p.Interact}
			}
			return entry(op.RunnerAddParams{Provider: p.Provider, Count: p.Count})
		},
		RunnerRemove: func(p op.RunnerRemoveParams) op.Operation { return entry(p) },
		Login: func(p op.LoginParams) op.Operation {
			if p.Interact == nil || p.Terminal != nil {
				t.Errorf("login params = %+v", p)
			}
			entry(op.LoginParams{Provider: p.Provider})
			return &loginOp{fakeOp: h.next(), ia: p.Interact, gap: h.gap}
		},
		Init:    func(p op.RepoParams) op.Operation { return entry("init:" + p.Repo) },
		Upgrade: func(p op.RepoParams) op.Operation { return entry("upgrade:" + p.Repo) },
		Status:  func(p op.RepoParams) op.Operation { return entry("status:" + p.Repo) },
		Pause:   func(p op.IssuesParams) op.Operation { return entry(p) },
		Resume:  func(p op.IssuesParams) op.Operation { return entry(p) },
	}
	ready := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	go func() {
		h.done <- Serve(ctx, Options{
			Ops:      ops,
			Commands: commands,
			ResolveRepo: func(v string) (string, error) {
				if v == "bad" {
					return "", op.Errorf(op.CodeInvalidRepo, "invalid repository %q", v)
				}
				if v == "" {
					v = "cur/repo"
				}
				return "github.com/" + v, nil
			},
			Ready: func(u string) { ready <- u },
			SessionOpened: func() {
				h.mu.Lock()
				h.opened++
				h.mu.Unlock()
			},
		})
	}()
	u := <-ready
	t.Cleanup(func() {
		cancel()
		if err := <-h.done; err != nil {
			t.Errorf("Serve = %v", err)
		}
	})
	base, proof, ok := strings.Cut(u, "/#proof=")
	if !ok || !strings.HasPrefix(base, "http://127.0.0.1:") || proof == "" {
		t.Fatalf("session URL %q", u)
	}
	h.base, h.proof, h.host = base, proof, strings.TrimPrefix(base, "http://")
	return h
}

// open exchanges the proof for the session.
func (h *harness) open() {
	h.t.Helper()
	status, body, _ := h.do("POST", "/api/session", "", map[string]string{ProofHeader: h.proof})
	if status != http.StatusOK {
		h.t.Fatalf("exchange: %d %s", status, body)
	}
	var b struct{ Session string }
	if err := json.Unmarshal(body, &b); err != nil || b.Session == "" {
		h.t.Fatalf("exchange body %s", body)
	}
	h.session = b.Session
}

func (h *harness) request(method, path, body string, headers map[string]string) *http.Request {
	h.t.Helper()
	req, err := http.NewRequest(method, h.base+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if h.session != "" {
		req.Header.Set(SessionHeader, h.session)
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	return req
}

func (h *harness) do(method, path, body string, headers map[string]string) (int, []byte, http.Header) {
	h.t.Helper()
	resp, err := http.DefaultClient.Do(h.request(method, path, body, headers))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var b []byte
	b, err = readAll(resp)
	if err != nil {
		h.t.Fatal(err)
	}
	for k := range resp.Header {
		if strings.HasPrefix(k, "Access-Control-") {
			h.t.Errorf("%s %s sent %s", method, path, k)
		}
	}
	return resp.StatusCode, b, resp.Header
}

func readAll(resp *http.Response) ([]byte, error) {
	var sb strings.Builder
	_, err := bufio.NewReader(resp.Body).WriteTo(&sb)
	return []byte(sb.String()), err
}

func errCode(t *testing.T, body []byte) string {
	t.Helper()
	var b struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return b.Error.Code
}

func TestSessionProofIsSingleUse(t *testing.T) {
	h := start(t)
	// Before the exchange the API is closed.
	if status, body, _ := h.do("POST", "/api/commands/status/plan", "{}", nil); status != 401 || errCode(t, body) != op.CodeSessionRequired {
		t.Fatalf("before exchange: %d %s", status, body)
	}
	// The proof itself is not a session.
	if status, body, _ := h.do("POST", "/api/commands/status/plan", "{}", map[string]string{SessionHeader: h.proof}); status != 401 || errCode(t, body) != op.CodeSessionRequired {
		t.Fatalf("proof as session: %d %s", status, body)
	}
	// Wrong and missing proofs fail without consuming it.
	for _, p := range []string{"wrong", h.proof + "X", ""} {
		status, body, _ := h.do("POST", "/api/session", "", map[string]string{ProofHeader: p})
		if status != 401 || errCode(t, body) != op.CodeProofInvalid {
			t.Errorf("proof %q: %d %s", p, status, body)
		}
	}
	h.open()
	if h.opened != 1 {
		t.Errorf("SessionOpened called %d times", h.opened)
	}
	status, body, hdr := h.do("POST", "/api/session", "", map[string]string{ProofHeader: h.proof})
	if status != 401 || errCode(t, body) != op.CodeProofInvalid || hdr.Get("Cache-Control") != "no-store" {
		t.Errorf("second exchange: %d %s", status, body)
	}
	if strings.Contains(string(body), h.session) {
		t.Error("the second exchange returned the session")
	}
	if status, body, _ := h.do("POST", "/api/commands/status/plan", "{}", nil); status != 200 {
		t.Errorf("with session: %d %s", status, body)
	}
	if status, body, _ := h.do("POST", "/api/commands/status/plan", "{}", map[string]string{SessionHeader: "wrong"}); status != 401 || errCode(t, body) != op.CodeSessionRequired {
		t.Errorf("wrong session: %d %s", status, body)
	}
	if h.opened != 1 {
		t.Errorf("SessionOpened called %d times", h.opened)
	}
}

func TestConcurrentExchangesOpenOneSession(t *testing.T) {
	h := start(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.DefaultClient.Do(h.request("POST", "/api/session", "", map[string]string{ProofHeader: h.proof}))
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == 200 {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 || h.opened != 1 {
		t.Errorf("%d exchanges succeeded, %d sessions opened", ok, h.opened)
	}
}

func TestHostAndOriginChecks(t *testing.T) {
	h := start(t)
	_, port, _ := net.SplitHostPort(h.host)
	other, _ := strconv.Atoi(port)
	for _, host := range []string{"localhost:" + port, "127.0.0.1:" + strconv.Itoa(other+1), "127.0.0.1", "evil.example:" + port} {
		for _, path := range []string{"/", "/api/session"} {
			status, body, _ := h.do("POST", path, "", map[string]string{"Host": host, ProofHeader: h.proof})
			if status != 403 || errCode(t, body) != op.CodeForbiddenHost {
				t.Errorf("Host %q %s: %d %s", host, path, status, body)
			}
		}
	}
	for _, origin := range []string{"http://localhost:" + port, "https://" + h.host, "http://evil.example", "null"} {
		status, body, _ := h.do("POST", "/api/session", "", map[string]string{"Origin": origin, ProofHeader: h.proof})
		if status != 403 || errCode(t, body) != op.CodeForbiddenOrigin {
			t.Errorf("Origin %q: %d %s", origin, status, body)
		}
	}
	// The refused requests did not consume the proof.
	status, _, _ := h.do("POST", "/api/session", "", map[string]string{"Origin": h.base, ProofHeader: h.proof})
	if status != 200 {
		t.Fatalf("same-origin exchange: %d", status)
	}
}

// withApp replaces the embedded UI for one test.
func withApp(t *testing.T, fsys fs.FS) {
	t.Helper()
	old := appFiles
	appFiles = fsys
	t.Cleanup(func() { appFiles = old })
}

func checkCommonHeaders(t *testing.T, what string, hdr http.Header) {
	t.Helper()
	for k, v := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff"} {
		if hdr.Get(k) != v {
			t.Errorf("%s: %s = %q", what, k, hdr.Get(k))
		}
	}
}

func TestPage(t *testing.T) {
	// A binary built without the Dashboard UI serves the placeholder.
	withApp(t, fstest.MapFS{".gitignore": {Data: []byte("*\n")}})
	h := start(t)
	status, body, hdr := h.do("GET", "/", "", nil)
	if status != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "not built yet") {
		t.Fatalf("page: %d %q %s", status, hdr.Get("Content-Type"), body)
	}
	checkCommonHeaders(t, "page", hdr)
	page := string(body)
	i, j := strings.Index(page, "<script>"), strings.Index(page, "</script>")
	if i < 0 || j < 0 || strings.Count(page, "<script") != 1 {
		t.Fatal("the page does not hold exactly one inline script")
	}
	sum := sha256.Sum256([]byte(page[i+len("<script>") : j]))
	csp := hdr.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") || !strings.HasPrefix(csp, "default-src 'none'") || !strings.Contains(csp, "connect-src 'self'") {
		t.Errorf("page CSP %q does not allow exactly the script", csp)
	}
	// Unknown paths outside the API are not found, with the API policy.
	for _, p := range []string{"/favicon.ico", "/.gitignore"} {
		status, body, hdr = h.do("GET", p, "", nil)
		if status != 404 || errCode(t, body) != op.CodeNotFound || hdr.Get("Content-Security-Policy") != apiCSP {
			t.Errorf("%s: %d %s, CSP %q", p, status, body, hdr.Get("Content-Security-Policy"))
		}
	}
}

func TestBuiltPage(t *testing.T) {
	withApp(t, fstest.MapFS{
		".gitignore":         {Data: []byte("*\n")},
		"index.html":         {Data: []byte(`<!doctype html><script type="module" src="/assets/x.js"></script>`)},
		"assets/x.js":        {Data: []byte("console.log(1)")},
		"assets/_chunk.js":   {Data: []byte("export {}")},
		"assets/x.css":       {Data: []byte("body{}")},
		"assets/x.woff2":     {Data: []byte("wOF2")},
		"assets/.hidden":     {Data: []byte("secret")},
		"licenses/OFL.txt":   {Data: []byte("OFL")},
		"api/commands/x.txt": {Data: []byte("not served")},
	})
	h := start(t)
	status, body, hdr := h.do("GET", "/", "", nil)
	if status != 200 || !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") || !strings.Contains(string(body), "/assets/x.js") {
		t.Fatalf("page: %d %q %s", status, hdr.Get("Content-Type"), body)
	}
	if hdr.Get("Content-Security-Policy") != appCSP {
		t.Errorf("page CSP %q", hdr.Get("Content-Security-Policy"))
	}
	checkCommonHeaders(t, "page", hdr)

	// Files need no session and carry the page's policy.
	for p, ctype := range map[string]string{
		"/assets/x.js":      "text/javascript; charset=utf-8",
		"/assets/_chunk.js": "text/javascript; charset=utf-8",
		"/assets/x.woff2":   "font/woff2",
		"/assets/x.css":     "text/css; charset=utf-8",
		"/licenses/OFL.txt": "text/plain; charset=utf-8",
	} {
		status, _, hdr := h.do("GET", p, "", nil)
		if status != 200 || hdr.Get("Content-Type") != ctype || hdr.Get("Content-Security-Policy") != appCSP {
			t.Errorf("%s: %d %q CSP %q", p, status, hdr.Get("Content-Type"), hdr.Get("Content-Security-Policy"))
		}
		checkCommonHeaders(t, p, hdr)
	}

	// Hidden files, folders, missing files and the index by name are not found.
	for _, p := range []string{"/.gitignore", "/assets/.hidden", "/assets/", "/assets", "/missing.js", "/favicon.ico"} {
		status, body, hdr := h.do("GET", p, "", nil)
		if status != 404 || errCode(t, body) != op.CodeNotFound || hdr.Get("Content-Security-Policy") != apiCSP {
			t.Errorf("%s: %d %s CSP %q", p, status, body, hdr.Get("Content-Security-Policy"))
		}
	}
	// Files are only read.
	if status, body, _ := h.do("POST", "/assets/x.js", "", nil); status != 404 || errCode(t, body) != op.CodeNotFound {
		t.Errorf("POST asset: %d %s", status, body)
	}

	// The API still needs the session, also under a path a file shares.
	for _, p := range []string{"/api/commands/x.txt", "/api/commands/status/plan", "/api/nope", "/api"} {
		for _, m := range []string{"GET", "POST"} {
			status, body, _ := h.do(m, p, "{}", nil)
			if status != 401 || errCode(t, body) != op.CodeSessionRequired {
				t.Errorf("%s %s without session: %d %s", m, p, status, body)
			}
		}
	}
	h.open()
	if status, body, _ := h.do("GET", "/api/commands/x.txt", "", nil); status != 404 || strings.Contains(string(body), "not served") {
		t.Errorf("file under /api/: %d %s", status, body)
	}

	// Host and Origin are enforced on files too.
	_, port, _ := net.SplitHostPort(h.host)
	if status, body, _ := h.do("GET", "/assets/x.js", "", map[string]string{"Host": "localhost:" + port}); status != 403 || errCode(t, body) != op.CodeForbiddenHost {
		t.Errorf("asset with wrong Host: %d %s", status, body)
	}
	if status, body, _ := h.do("GET", "/assets/x.js", "", map[string]string{"Origin": "http://evil.example"}); status != 403 || errCode(t, body) != op.CodeForbiddenOrigin {
		t.Errorf("asset with wrong Origin: %d %s", status, body)
	}
}

// The embedded folder always holds the committed placeholder, so go build
// works with or without npm run build.
func TestEmbeddedPlaceholder(t *testing.T) {
	if _, err := fs.Stat(appFiles, ".gitignore"); err != nil {
		t.Fatalf("dist/.gitignore is not embedded: %v", err)
	}
}

func TestBodyChecks(t *testing.T) {
	h := start(t)
	h.open()
	for name, body := range map[string]string{
		"unknown field": `{"repo":"o/r","extra":1}`,
		"malformed":     `{"repo":`,
		"trailing":      `{"repo":"o/r"} {}`,
		"oversized":     `{"repo":"` + strings.Repeat("a", maxBody) + `"}`,
		"hidden field":  `{"provider":"claude","Interact":{}}`,
	} {
		path := "/api/commands/status/plan"
		if name == "hidden field" {
			path = "/api/commands/login/plan"
		}
		status, b, _ := h.do("POST", path, body, nil)
		if status != 400 || errCode(t, b) != op.CodeInvalidArgument {
			t.Errorf("%s: %d %.200s", name, status, b)
		}
	}
	if status, b, _ := h.do("POST", "/api/commands/nope/plan", "{}", nil); status != 404 || errCode(t, b) != op.CodeNotFound {
		t.Errorf("unknown command: %d %s", status, b)
	}
	if len(h.params) != 0 {
		t.Errorf("an entry point ran: %v", h.params)
	}
}

func TestEveryCommandReachesItsEntry(t *testing.T) {
	h := start(t)
	h.open()
	for _, c := range []struct{ command, body string }{
		{"runner-add", `{"provider":"codex","count":3}`},
		{"runner-add", `{"provider":"claude"}`},
		{"runner-remove", `{"provider":"claude","count":2,"deleteLogin":true}`},
		{"runner-remove", `{"provider":"codex"}`},
		{"login", `{"provider":"claude"}`},
		{"login", ``},
		{"init", `{"repo":"o/r"}`},
		{"upgrade", `{}`},
		{"status", `{"repo":"x/y"}`},
		{"status", `{}`},
		{"pause", `{"issues":["4","#5"]}`},
		{"resume", `{"repo":"o/r","issues":["6"]}`},
		{"pause", `{"issues":["https://github.com/a/b/issues/7"]}`},
		{"resume", `{"repo":"x/y","issues":["https://github.com/a/b/issues/8"]}`},
	} {
		if status, b, _ := h.do("POST", "/api/commands/"+c.command+"/plan", c.body, nil); status != 200 {
			t.Errorf("%s %s: %d %s", c.command, c.body, status, b)
		}
	}
	want := []any{
		op.RunnerAddParams{Provider: "codex", Count: 3},
		op.RunnerAddParams{Provider: "claude", Count: op.DefaultRunnerAddCount},
		op.RunnerRemoveParams{Provider: "claude", Count: 2, DeleteLogin: true},
		op.RunnerRemoveParams{Provider: "codex"},
		op.LoginParams{Provider: "claude"},
		op.LoginParams{},
		"init:github.com/o/r",
		"upgrade:github.com/cur/repo",
		"status:github.com/x/y",
		"status:",
		op.IssuesParams{Repo: "github.com/cur/repo", Issues: []string{"4", "#5"}},
		op.IssuesParams{Repo: "github.com/o/r", Issues: []string{"6"}},
		op.IssuesParams{Issues: []string{"https://github.com/a/b/issues/7"}},
		op.IssuesParams{Repo: "github.com/x/y", Issues: []string{"https://github.com/a/b/issues/8"}},
	}
	if !reflect.DeepEqual(h.params, want) {
		t.Errorf("params = %#v\nwant %#v", h.params, want)
	}
	status, b, _ := h.do("POST", "/api/commands/init/plan", `{"repo":"bad"}`, nil)
	if status != 422 || errCode(t, b) != op.CodeInvalidRepo {
		t.Errorf("bad repo: %d %s", status, b)
	}
}

type planned struct {
	ID     string
	Plan   op.Plan
	Result op.Result
}

func (h *harness) plan(command, body string) planned {
	h.t.Helper()
	status, b, _ := h.do("POST", "/api/commands/"+command+"/plan", body, nil)
	if status != 200 {
		h.t.Fatalf("plan %s: %d %s", command, status, b)
	}
	var p planned
	if err := json.Unmarshal(b, &p); err != nil {
		h.t.Fatal(err)
	}
	return p
}

type event struct {
	Event  string
	Step   op.LoginStep
	Result op.Result
	Error  struct{ Code, Message string }
}

func (h *harness) apply(id, body string) (int, []event) {
	h.t.Helper()
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/plans/"+id+"/apply", body, nil))
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var events []event
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			h.t.Fatalf("line %q: %v", sc.Text(), err)
		}
		events = append(events, e)
	}
	return resp.StatusCode, events
}

func TestReadOnlyPlanReturnsItsResult(t *testing.T) {
	h := start(t)
	h.open()
	o := &fakeOp{plan: op.Plan{Operation: "status", ReadOnly: true}}
	h.next = func() *fakeOp { return o }
	p := h.plan("status", `{}`)
	if p.ID != "" || p.Result["operation"] != "status" || !p.Plan.ReadOnly {
		t.Errorf("read-only plan = %+v", p)
	}
	if !reflect.DeepEqual(o.Calls(), []string{"plan", "readback"}) {
		t.Errorf("calls = %v", o.Calls())
	}
	o.err = op.Errorf(op.CodeAuthUnavailable, "no gh login")
	status, b, _ := h.do("POST", "/api/commands/status/plan", `{}`, nil)
	if status != 422 || errCode(t, b) != op.CodeAuthUnavailable {
		t.Errorf("plan error: %d %s", status, b)
	}
}

func TestConfirmAndApply(t *testing.T) {
	h := start(t)
	h.open()
	o := &fakeOp{plan: op.Plan{Operation: "upgrade", Confirm: true}}
	h.next = func() *fakeOp { return o }
	p := h.plan("upgrade", `{"repo":"o/r"}`)
	if p.ID == "" || p.Plan.Operation != "upgrade" {
		t.Fatalf("plan = %+v", p)
	}
	status, events := h.apply(p.ID, `{}`)
	if status != 200 || len(events) != 1 || events[0].Event != "error" || events[0].Error.Code != op.CodeConfirmationRequired {
		t.Fatalf("unconfirmed: %d %+v", status, events)
	}
	if !reflect.DeepEqual(o.Calls(), []string{"plan"}) {
		t.Fatalf("unconfirmed calls = %v", o.Calls())
	}
	status, events = h.apply(p.ID, `{"confirm":true}`)
	if status != 200 || len(events) != 1 || events[0].Event != "result" || events[0].Result["operation"] != "upgrade" {
		t.Fatalf("confirmed: %d %+v", status, events)
	}
	if !reflect.DeepEqual(o.Calls(), []string{"plan", "apply", "readback"}) {
		t.Errorf("calls = %v", o.Calls())
	}
	if status, b, _ := h.do("POST", "/api/plans/"+p.ID+"/apply", `{"confirm":true}`, nil); status != 404 || errCode(t, b) != op.CodeNotFound {
		t.Errorf("second apply: %d %s", status, b)
	}

	// An apply error keeps its code; the plan is gone afterwards.
	o = &fakeOp{plan: op.Plan{Operation: "pause"}, apply: func(context.Context) error {
		return op.Wrap(op.CodeGitHubError, op.Errorf(op.CodeLabelMissing, "no pause label"))
	}}
	p = h.plan("pause", `{"issues":["https://github.com/a/b/issues/1"]}`)
	if _, events = h.apply(p.ID, ``); len(events) != 1 || events[0].Error.Code != op.CodeLabelMissing {
		t.Errorf("apply error: %+v", events)
	}

	// DELETE drops a stored plan.
	p = h.plan("pause", `{"issues":["https://github.com/a/b/issues/1"]}`)
	if status, _, _ := h.do("DELETE", "/api/plans/"+p.ID, "", nil); status != 204 {
		t.Errorf("delete: %d", status)
	}
	if status, _, _ := h.do("DELETE", "/api/plans/"+p.ID, "", nil); status != 404 {
		t.Errorf("second delete: %d", status)
	}
}

// loginOp is a fake login: Apply emits the G1 steps and waits for a code.
type loginOp struct {
	*fakeOp
	ia   op.Interactor
	code string
	// gap, when set, holds Apply between code_requested and Code, as the
	// real login does while it flushes the step before calling Code.
	gap chan struct{}
}

func (l *loginOp) Apply(ctx context.Context, _ op.Plan) error {
	l.record("apply")
	l.ia.Step(op.LoginStep{Provider: "claude", Kind: op.StepOpenURL, URL: "https://example.com/login"})
	l.ia.Step(op.LoginStep{Provider: "claude", Kind: op.StepDeviceCode, Code: "ABCD-EFGH"})
	l.ia.Step(op.LoginStep{Provider: "claude", Kind: op.StepCodeRequested})
	if l.gap != nil {
		<-l.gap
	}
	code, err := l.ia.Code(ctx)
	if err != nil {
		l.ia.Step(op.LoginStep{Provider: "claude", Kind: op.StepFailed, Message: err.Error()})
		return err
	}
	l.code = code
	l.ia.Step(op.LoginStep{Provider: "claude", Kind: op.StepSucceeded})
	return nil
}

func TestLoginStreamsStepsAndTakesTheCode(t *testing.T) {
	h := start(t)
	h.open()
	o := &fakeOp{plan: op.Plan{Operation: "login"}}
	h.next = func() *fakeOp { return o }
	p := h.plan("login", `{"provider":"claude"}`)
	if status, b, _ := h.do("POST", "/api/plans/"+p.ID+"/code", `{"code":"early"}`, nil); status != 409 || errCode(t, b) != op.CodeInvalidArgument {
		t.Errorf("code before code_requested: %d %s", status, b)
	}

	resp, err := http.DefaultClient.Do(h.request("POST", "/api/plans/"+p.ID+"/apply", `{}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("Content-Type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	var kinds []string
	for len(kinds) < 3 && sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Event != "login_step" {
			t.Fatalf("line %q", sc.Text())
		}
		kinds = append(kinds, e.Step.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{op.StepOpenURL, op.StepDeviceCode, op.StepCodeRequested}) {
		t.Fatalf("steps = %v", kinds)
	}
	// While it waits another apply is refused.
	other := h.plan("pause", `{"issues":["https://github.com/a/b/issues/1"]}`)
	if status, b, _ := h.do("POST", "/api/plans/"+other.ID+"/apply", `{}`, nil); status != 409 || errCode(t, b) != op.CodeOperationRunning || !strings.Contains(string(b), "login") {
		t.Errorf("concurrent apply: %d %s", status, b)
	}
	if status, b, _ := h.do("DELETE", "/api/plans/"+p.ID, "", nil); status != 409 {
		t.Errorf("delete while applying: %d %s", status, b)
	}
	if status, b, _ := h.do("POST", "/api/plans/"+p.ID+"/code", `{"code":"abc-123"}`, nil); status != 204 {
		t.Fatalf("code: %d %s", status, b)
	}
	var rest []event
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		rest = append(rest, e)
	}
	if len(rest) != 2 || rest[0].Step.Kind != op.StepSucceeded || rest[1].Event != "result" {
		t.Fatalf("after code: %+v", rest)
	}
	// The other plan stayed stored and applies now.
	if _, events := h.apply(other.ID, `{}`); len(events) != 1 || events[0].Event != "result" {
		t.Errorf("other plan: %+v", events)
	}
}

// The code may arrive after code_requested is streamed but before the login
// reaches Code: it is accepted and handed over once Code runs.
func TestCodePostedBeforeTheLoginWaits(t *testing.T) {
	h := start(t)
	h.open()
	gap := make(chan struct{})
	h.gap = gap
	o := &fakeOp{plan: op.Plan{Operation: "login"}}
	h.next = func() *fakeOp { return o }
	p := h.plan("login", `{"provider":"claude"}`)
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/plans/"+p.ID+"/apply", `{}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && !strings.Contains(sc.Text(), op.StepCodeRequested) {
	}
	// Apply is held in the gap, so Code has not been called yet.
	if status, b, _ := h.do("POST", "/api/plans/"+p.ID+"/code", `{"code":"abc-123"}`, nil); status != 204 {
		t.Fatalf("code in the gap: %d %s", status, b)
	}
	if status, b, _ := h.do("POST", "/api/plans/"+p.ID+"/code", `{"code":"again"}`, nil); status != 409 || errCode(t, b) != op.CodeInvalidArgument {
		t.Errorf("second code: %d %s", status, b)
	}
	close(gap)
	var rest []event
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		rest = append(rest, e)
	}
	if len(rest) != 2 || rest[0].Step.Kind != op.StepSucceeded || rest[1].Event != "result" {
		t.Fatalf("after code: %+v", rest)
	}
}

func TestRunnerAddLoginUsesTheInteractor(t *testing.T) {
	h := start(t)
	h.open()
	lo := &fakeOp{plan: op.Plan{Operation: "runner add"}, apply: func(context.Context) error { return nil }}
	h.next = func() *fakeOp { return lo }
	p := h.plan("runner-add", `{"provider":"claude"}`)
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/plans/"+p.ID+"/apply", `{}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	lines := 0
	// Feed the code once it is requested.
	for sc.Scan() {
		lines++
		if strings.Contains(sc.Text(), op.StepCodeRequested) {
			if status, b, _ := h.do("POST", "/api/plans/"+p.ID+"/code", `{"code":"x"}`, nil); status != 204 {
				t.Fatalf("code: %d %s", status, b)
			}
		}
		if strings.Contains(sc.Text(), `"event":"result"`) && lines != 5 {
			t.Errorf("result after %d lines", lines)
		}
	}
	if lines != 5 {
		t.Errorf("%d lines", lines)
	}
}

func TestDisconnectCancelsTheLogin(t *testing.T) {
	h := start(t)
	h.open()
	o := &fakeOp{plan: op.Plan{Operation: "login"}}
	h.next = func() *fakeOp { return o }
	p := h.plan("login", `{"provider":"claude"}`)
	ctx, cancel := context.WithCancel(context.Background())
	req := h.request("POST", "/api/plans/"+p.ID+"/apply", `{}`, nil).WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	for i := 0; i < 3 && sc.Scan(); i++ {
	}
	cancel()
	resp.Body.Close()
	// The cancelled apply releases the lock; a new plan applies.
	h.next = func() *fakeOp { return &fakeOp{plan: op.Plan{Operation: "pause"}} }
	next := h.plan("pause", `{"issues":["https://github.com/a/b/issues/1"]}`)
	for {
		status, events := h.apply(next.ID, `{}`)
		if status == 200 && len(events) == 1 && events[0].Event == "result" {
			break
		}
		if status != 409 {
			t.Fatalf("apply after disconnect: %d %+v", status, events)
		}
	}
	if !reflect.DeepEqual(o.Calls(), []string{"plan", "apply"}) {
		t.Errorf("login calls = %v (read back after a cancelled code)", o.Calls())
	}
}

func TestServeStopCancelsAnApply(t *testing.T) {
	h := start(t)
	h.open()
	o := &fakeOp{plan: op.Plan{Operation: "login"}}
	h.next = func() *fakeOp { return o }
	p := h.plan("login", `{"provider":"claude"}`)
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/plans/"+p.ID+"/apply", `{}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for i := 0; i < 3 && sc.Scan(); i++ {
	}
	h.cancel()
	var last event
	for sc.Scan() {
		if err := json.Unmarshal(sc.Bytes(), &last); err != nil {
			t.Fatal(err)
		}
	}
	if last.Event != "error" || last.Error.Code != op.CodeCancelled {
		t.Errorf("last line = %+v", last)
	}
}

func TestServeListenErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if err := Serve(context.Background(), Options{Port: port}); op.Code(err) != op.CodeListenFailed {
		t.Errorf("port in use: %v", err)
	}
	for _, p := range []int{-1, 65536} {
		if err := Serve(context.Background(), Options{Port: p}); op.Code(err) != op.CodeInvalidArgument {
			t.Errorf("port %d: %v", p, err)
		}
	}
}

func TestServeUsesTheGivenPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var got string
	err = Serve(ctx, Options{Port: port, Ready: func(u string) { got = u; cancel() }})
	if err != nil || !strings.HasPrefix(got, "http://127.0.0.1:"+strconv.Itoa(port)+"/#proof=") {
		t.Errorf("Serve = %v, URL %q", err, got)
	}
}

func TestCommandsServesTheSchema(t *testing.T) {
	want := []Command{{
		Path:  "runner add",
		API:   "runner-add",
		Short: "Add Runners",
		Flags: []Flag{{Name: "count", Type: "int", Default: "2", Usage: "number of Runners", Param: "count"}},
		Args:  []Arg{{Name: "provider", Required: true, Allowed: []string{"claude", "codex"}, Param: "provider"}},
	}}
	h := startWith(t, want)

	// Without the session it is refused like every other /api/ route.
	status, body, _ := h.do("GET", "/api/commands", "", nil)
	if status != http.StatusUnauthorized || errCode(t, body) != op.CodeSessionRequired {
		t.Fatalf("no session: %d %s", status, body)
	}
	status, body, _ = h.do("GET", "/api/commands", "", map[string]string{"Host": "localhost:1"})
	if status != http.StatusForbidden || errCode(t, body) != op.CodeForbiddenHost {
		t.Errorf("wrong Host: %d %s", status, body)
	}

	h.open()
	status, body, _ = h.do("GET", "/api/commands", "", map[string]string{"Origin": "http://evil.example"})
	if status != http.StatusForbidden || errCode(t, body) != op.CodeForbiddenOrigin {
		t.Errorf("wrong Origin: %d %s", status, body)
	}
	status, body, hdr := h.do("GET", "/api/commands", "", nil)
	if status != http.StatusOK || hdr.Get("Content-Type") != "application/json" {
		t.Fatalf("GET: %d %s %v", status, body, hdr)
	}
	var got struct{ Commands []Command }
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Commands, want) {
		t.Errorf("commands = %+v, want %+v", got.Commands, want)
	}
	if !strings.Contains(string(body), `"path":"runner add"`) || !strings.Contains(string(body), `"repeatable":false`) {
		t.Errorf("body %s", body)
	}

	// The route only reads.
	status, body, _ = h.do("POST", "/api/commands", "", nil)
	if status != http.StatusNotFound || errCode(t, body) != op.CodeNotFound {
		t.Errorf("POST: %d %s", status, body)
	}
}

func TestCommandsWithoutASchemaIsAnEmptyList(t *testing.T) {
	h := start(t)
	h.open()
	status, body, _ := h.do("GET", "/api/commands", "", nil)
	if status != http.StatusOK || strings.TrimSpace(string(body)) != `{"commands":[]}` {
		t.Errorf("%d %s", status, body)
	}
}
