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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/status"
)

// fakeOp records its calls; apply, when set, runs as its Apply.
type fakeOp struct {
	mu    sync.Mutex
	plan  op.Plan
	err   error
	apply func(ctx context.Context) error
	// readBack, when set, runs as its ReadBack.
	readBack func() (op.Result, error)
	// readCtx, when set, runs as its ReadBack with the context.
	readCtx func(context.Context) (op.Result, error)
	// planCtx, when set, runs as its Plan with the context.
	planCtx func(context.Context) (op.Plan, error)
	calls   []string
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

func (f *fakeOp) Plan(ctx context.Context) (op.Plan, error) {
	f.record("plan")
	if f.planCtx != nil {
		return f.planCtx(ctx)
	}
	return f.plan, f.err
}
func (f *fakeOp) Apply(ctx context.Context, _ op.Plan) error {
	f.record("apply")
	if f.apply != nil {
		return f.apply(ctx)
	}
	return nil
}
func (f *fakeOp) ReadBack(ctx context.Context, _ op.Plan) (op.Result, error) {
	f.record("readback")
	if f.readCtx != nil {
		return f.readCtx(ctx)
	}
	if f.readBack != nil {
		return f.readBack()
	}
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
	return startFull(t, commands, nil)
}

// startFull starts the server with commands and watch.
func startFull(t *testing.T, commands []Command, watch func(context.Context, func(any)) error, more ...func(*Options)) *harness {
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
	o := Options{
		Ops:      ops,
		Commands: commands,
		Watch:    watch,
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
	}
	for _, m := range more {
		m(&o)
	}
	go func() { h.done <- Serve(ctx, o) }()
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
	Event    string
	Loading  []string
	Progress op.Event
	Step     op.LoginStep
	Result   op.Result
	Error    struct{ Code, Message string }
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

// The built page and every HTML file with the placeholder get a fresh
// style-src nonce per response, the same in the header and the body.
func TestBuiltPageNonce(t *testing.T) {
	page := `<!doctype html><meta property="csp-nonce" nonce="NIBFIN_CSP_NONCE"><script type="module" nonce="NIBFIN_CSP_NONCE" src="/assets/x.js"></script><link rel="stylesheet" nonce="NIBFIN_CSP_NONCE" href="/assets/x.css">`
	withApp(t, fstest.MapFS{
		"index.html":  {Data: []byte(page)},
		"other.html":  {Data: []byte(page)},
		"plain.html":  {Data: []byte(`<!doctype html><p>no nonce</p>`)},
		"assets/x.js": {Data: []byte("console.log('NIBFIN_CSP_NONCE')")},
	})
	h := start(t)
	nonceRE := regexp.MustCompile(`style-src 'self' 'nonce-([A-Z2-7]+)'; `)
	seen := map[string]bool{}
	for _, p := range []string{"/", "/", "/other.html"} {
		status, body, hdr := h.do("GET", p, "", nil)
		csp := hdr.Get("Content-Security-Policy")
		m := nonceRE.FindStringSubmatch(csp)
		if status != 200 || m == nil || hdr.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: %d %q CSP %q", p, status, hdr.Get("Content-Type"), csp)
		}
		nonce := m[1]
		if csp != strings.Replace(appCSP, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1) {
			t.Errorf("%s: CSP %q changes more than style-src", p, csp)
		}
		if !strings.Contains(csp, "script-src 'self'; ") || strings.Contains(csp, "unsafe") {
			t.Errorf("%s: CSP %q", p, csp)
		}
		if strings.Contains(string(body), noncePlaceholder) || strings.Count(string(body), `nonce="`+nonce+`"`) != 3 {
			t.Errorf("%s: body %s does not carry nonce %s", p, body, nonce)
		}
		if seen[nonce] {
			t.Errorf("%s: nonce %s repeated", p, nonce)
		}
		seen[nonce] = true
		checkCommonHeaders(t, p, hdr)
	}

	// HTML without the placeholder and other files keep appCSP unchanged.
	status, body, hdr := h.do("GET", "/plain.html", "", nil)
	if status != 200 || hdr.Get("Content-Security-Policy") != appCSP || string(body) != `<!doctype html><p>no nonce</p>` {
		t.Errorf("plain.html: %d CSP %q %s", status, hdr.Get("Content-Security-Policy"), body)
	}
	status, body, hdr = h.do("GET", "/assets/x.js", "", nil)
	if status != 200 || hdr.Get("Content-Security-Policy") != appCSP || !strings.Contains(string(body), noncePlaceholder) {
		t.Errorf("x.js: %d CSP %q %s", status, hdr.Get("Content-Security-Policy"), body)
	}
	// The index by name still redirects to its folder, which serves the page.
	status, _, hdr = h.do("GET", "/index.html", "", nil)
	if status != 200 || nonceRE.FindString(hdr.Get("Content-Security-Policy")) == "" {
		t.Errorf("index.html: %d CSP %q", status, hdr.Get("Content-Security-Policy"))
	}
}

func TestApplyStreamsProgressAndTheResultWithTheError(t *testing.T) {
	h := start(t)
	h.open()
	step := op.Step{ID: "issue:x", Action: "add label", Target: "x"}
	partial := op.Result{op.ResultItems: []op.Check{{ID: "issue:x", Action: "add label", Target: "x", Reason: "has bug"}}, op.ResultNotes: []string{}}
	o := &fakeOp{plan: op.Plan{Operation: "pause", Steps: []op.Step{step}},
		apply: func(ctx context.Context) error {
			return op.Track(ctx, step, func() error { return nil })
		},
		readBack: func() (op.Result, error) { return partial, op.Errorf(op.CodeNotApplied, "pause not applied") }}
	h.next = func() *fakeOp { return o }
	p := h.plan("pause", `{"issues":["https://github.com/a/b/issues/1"]}`)
	status, events := h.apply(p.ID, ``)
	if status != 200 || len(events) != 3 {
		t.Fatalf("%d %+v", status, events)
	}
	if events[0].Event != "progress" || events[0].Progress.Phase != op.PhaseStarted || events[0].Progress.ID != "issue:x" || events[0].Progress.Target != "x" ||
		events[1].Event != "progress" || events[1].Progress.Phase != op.PhaseDone {
		t.Errorf("progress = %+v", events[:2])
	}
	last := events[2]
	if last.Event != "error" || last.Error.Code != op.CodeNotApplied || last.Result == nil {
		t.Fatalf("error line = %+v", last)
	}
	items, _ := last.Result[op.ResultItems].([]any)
	if len(items) != 1 || items[0].(map[string]any)["confirmed"] != false || items[0].(map[string]any)["reason"] != "has bug" {
		t.Errorf("result = %v", last.Result)
	}

	// A failed item carries its code; an error without a result has no
	// result field.
	o = &fakeOp{plan: op.Plan{Operation: "pause"}, apply: func(ctx context.Context) error {
		return op.Track(ctx, step, func() error { return op.Errorf(op.CodeGitHubError, "boom") })
	}}
	p = h.plan("pause", `{"issues":["https://github.com/a/b/issues/1"]}`)
	_, events = h.apply(p.ID, ``)
	if len(events) != 3 || events[1].Progress.Phase != op.PhaseFailed || events[1].Progress.Code != op.CodeGitHubError || events[1].Progress.Message != "boom" ||
		events[2].Event != "error" || events[2].Result != nil {
		t.Errorf("failed apply = %+v", events)
	}
}

func TestReadOnlyChangingPlanReadBackFailure(t *testing.T) {
	h := start(t)
	h.open()
	steps := []op.Step{{ID: "pr", Action: "keep PR", Target: "#3", NoChange: true}}
	partial := op.Result{"repo": "github.com/o/r", op.ResultItems: []op.Check{{ID: "pr", Action: "keep PR", Target: "#3", Reason: "closed"}}, op.ResultNotes: []string{}}
	o := &fakeOp{plan: op.Plan{Operation: "upgrade", Steps: steps, ReadOnly: true}}
	h.next = func() *fakeOp { return o }

	// Success: {"plan","result"} as for status.
	p := h.plan("upgrade", `{"repo":"o/r"}`)
	if p.ID != "" || !p.Plan.ReadOnly || p.Result["operation"] != "upgrade" || len(p.Plan.Steps) != 1 || !p.Plan.Steps[0].NoChange {
		t.Errorf("read-only plan = %+v", p)
	}

	// A failed read-back keeps 422 and the error body, with the plan and
	// the result read so far.
	o.readBack = func() (op.Result, error) { return partial, op.Errorf(op.CodeRepoWriteFailed, "the PR is closed") }
	status, b, _ := h.do("POST", "/api/commands/upgrade/plan", `{"repo":"o/r"}`, nil)
	if status != 422 || errCode(t, b) != op.CodeRepoWriteFailed {
		t.Fatalf("%d %s", status, b)
	}
	var body struct {
		Plan   op.Plan
		Result struct{ Items []op.Check }
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if body.Plan.Operation != "upgrade" || len(body.Plan.Steps) != 1 || len(body.Result.Items) != 1 || body.Result.Items[0].Reason != "closed" {
		t.Errorf("body = %s", b)
	}

	// Without a result there is no result field.
	o.readBack = func() (op.Result, error) { return nil, op.Errorf(op.CodeRepoUnreadable, "offline") }
	status, b, _ = h.do("POST", "/api/commands/upgrade/plan", `{"repo":"o/r"}`, nil)
	if status != 422 || errCode(t, b) != op.CodeRepoUnreadable || !strings.Contains(string(b), `"plan"`) || strings.Contains(string(b), `"result"`) {
		t.Errorf("%d %s", status, b)
	}
}

// read posts body to the status stream route and returns its lines.
func (h *harness) read(body string, headers map[string]string) (int, string, []event) {
	h.t.Helper()
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/commands/status/read", body, headers))
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
	return resp.StatusCode, resp.Header.Get("Content-Type"), events
}

func TestStatusReadStreams(t *testing.T) {
	h := start(t)
	// The session and origin checks of every /api/ route.
	if status, b, _ := h.do("POST", "/api/commands/status/read", `{"repo":"o/r"}`, nil); status != 401 || errCode(t, b) != op.CodeSessionRequired {
		t.Errorf("without session: %d %s", status, b)
	}
	h.open()
	if status, b, _ := h.do("POST", "/api/commands/status/read", `{"repo":"o/r"}`, map[string]string{"Origin": "http://evil.example"}); status != 403 || errCode(t, b) != op.CodeForbiddenOrigin {
		t.Errorf("wrong origin: %d %s", status, b)
	}
	if status, b, _ := h.do("GET", "/api/commands/status/read", "", nil); status != 405 && status != 404 {
		t.Errorf("GET: %d %s", status, b)
	}

	o := &fakeOp{plan: op.Plan{Operation: "status", ReadOnly: true}}
	o.readCtx = func(ctx context.Context) (op.Result, error) {
		sink := status.PartialFrom(ctx)
		if sink == nil {
			t.Error("no partial sink")
			return op.Result{}, nil
		}
		sink(op.Result{"issues": json.RawMessage(`[]`)}, []string{"host", "image"})
		sink(op.Result{"issues": json.RawMessage(`[]`), "image": json.RawMessage(`{}`)}, []string{"host"})
		return op.Result{"repo": "github.com/o/r"}, nil
	}
	h.next = func() *fakeOp { return o }
	code, ctype, events := h.read(`{"repo":"o/r"}`, nil)
	if code != 200 || ctype != "application/x-ndjson" || len(events) != 3 {
		t.Fatalf("read: %d %q %+v", code, ctype, events)
	}
	if e := events[0]; e.Event != "partial" || !reflect.DeepEqual(e.Loading, []string{"host", "image"}) || e.Result["issues"] == nil {
		t.Errorf("first = %+v", e)
	}
	if e := events[1]; e.Event != "partial" || !reflect.DeepEqual(e.Loading, []string{"host"}) {
		t.Errorf("second = %+v", e)
	}
	if e := events[2]; e.Event != "result" || e.Result["repo"] != "github.com/o/r" {
		t.Errorf("last = %+v", e)
	}
	h.mu.Lock()
	last := h.params[len(h.params)-1]
	h.mu.Unlock()
	if last != "status:github.com/o/r" {
		t.Errorf("params = %v", last)
	}

	// A failed read: the error with what was read.
	o.readCtx = func(context.Context) (op.Result, error) {
		return op.Result{"repo": "x"}, op.Errorf(op.CodeAuthUnavailable, "no gh login")
	}
	if _, _, events := h.read(`{"repo":"o/r"}`, nil); len(events) != 1 || events[0].Event != "error" ||
		events[0].Error.Code != op.CodeAuthUnavailable || events[0].Result["repo"] != "x" {
		t.Errorf("failed read = %+v", events)
	}

	// A bad repository or body fails before the stream.
	if status, b, _ := h.do("POST", "/api/commands/status/read", `{"repo":"bad"}`, nil); status != 422 || errCode(t, b) != op.CodeInvalidRepo {
		t.Errorf("bad repo: %d %s", status, b)
	}
	if status, b, _ := h.do("POST", "/api/commands/status/read", `{"nope":1}`, nil); status != 400 || errCode(t, b) != op.CodeInvalidArgument {
		t.Errorf("bad body: %d %s", status, b)
	}
	// A plan that is not a read is never applied.
	w := &fakeOp{plan: op.Plan{Operation: "init"}}
	h.next = func() *fakeOp { return w }
	if status, b, _ := h.do("POST", "/api/commands/status/read", `{}`, nil); status != 422 || !reflect.DeepEqual(w.Calls(), []string{"plan"}) {
		t.Errorf("not a read: %d %s %v", status, b, w.Calls())
	}
}

func TestStatusReadCancels(t *testing.T) {
	h := start(t)
	h.open()
	ended := make(chan error, 1)
	o := &fakeOp{plan: op.Plan{Operation: "status", ReadOnly: true}}
	o.readCtx = func(ctx context.Context) (op.Result, error) {
		status.PartialFrom(ctx)(op.Result{}, []string{"host"})
		<-ctx.Done()
		ended <- ctx.Err()
		return nil, ctx.Err()
	}
	h.next = func() *fakeOp { return o }
	resp, err := http.DefaultClient.Do(h.request("POST", "/api/commands/status/read", `{"repo":"o/r"}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	if !sc.Scan() || !strings.Contains(sc.Text(), `"partial"`) {
		t.Fatalf("first line %q", sc.Text())
	}
	resp.Body.Close()
	select {
	case err := <-ended:
		if err == nil {
			t.Error("read not cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closing the request did not cancel the read")
	}
}
