package op

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/armor"
	"github.com/cli/go-gh/v2/pkg/api"
	"golang.org/x/crypto/ssh"

	"github.com/taco3064/gh-nibfin/internal/runnerreq"
)

// fakeRequest is the request Issue side of the fake GitHub.
type fakeRequest struct {
	keys    []ghKey
	keysErr error
	// bodies are the request bodies opened; issue is the last one.
	bodies []string
	titles []string
	issue  ghIssue
	// answer returns the comments at the n-th read (1-based).
	answer func(n int) []ghComment
	reads  int
	// closeAt closes the Issue (by someone else) at that read.
	closeAt  int
	comments []string
	closes   []string
	closeErr error
	// onRead runs at each comments read.
	onRead func(n int)
}

func (g *fakeRunnerAPI) UserKeys(_ context.Context, login string) ([]ghKey, error) {
	if g.req == nil {
		return nil, nil
	}
	return g.req.keys, g.req.keysErr
}

func (g *fakeRunnerAPI) CreateIssue(_ context.Context, repo, title, body string) (ghIssue, error) {
	r := g.req
	r.titles, r.bodies = append(r.titles, title), append(r.bodies, body)
	r.issue = ghIssue{Number: 7, HTMLURL: "https://" + repo + "/issues/7", State: "open"}
	g.host.events = append(g.host.events, "request")
	return r.issue, nil
}

func (g *fakeRunnerAPI) Issue(_ context.Context, repo string, n int) (ghIssue, error) {
	return g.req.issue, nil
}

func (g *fakeRunnerAPI) IssueComments(_ context.Context, repo string, n int) ([]ghComment, error) {
	r := g.req
	r.reads++
	if r.onRead != nil {
		r.onRead(r.reads)
	}
	if r.closeAt > 0 && r.reads >= r.closeAt {
		r.issue.State = "closed"
	}
	if r.answer == nil {
		return nil, nil
	}
	return r.answer(r.reads), nil
}

func (g *fakeRunnerAPI) CreateComment(_ context.Context, repo string, n int, body string) error {
	g.req.comments = append(g.req.comments, body)
	g.host.events = append(g.host.events, "comment")
	return nil
}

func (g *fakeRunnerAPI) CloseIssue(_ context.Context, repo string, n int, reason string) error {
	g.host.events = append(g.host.events, "close "+reason)
	if g.req.closeErr != nil {
		return g.req.closeErr
	}
	g.req.closes = append(g.req.closes, reason)
	g.req.issue.State = "closed"
	return nil
}

// testKey is a generated key pair written to the fake ~/.ssh.
type testKey struct {
	pub, priv  string
	authorized string
}

// writeKey generates a key of typ (ed25519 or rsa) as dir/name and
// dir/name.pub, with passphrase when not empty.
func writeKey(t *testing.T, dir, name, typ, passphrase string) testKey {
	t.Helper()
	var priv any
	switch typ {
	case "ed25519":
		_, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		priv = k
	case "rsa":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		priv = k
	}
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0o700)
	k := testKey{pub: filepath.Join(dir, name+".pub"), priv: filepath.Join(dir, name),
		authorized: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))}
	if err := os.WriteFile(k.priv, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k.pub, []byte(k.authorized+" me@host\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return k
}

// encryptTo encrypts token to the SSH key as the workflow's age -a -R does.
func encryptTo(t *testing.T, authorized, token string) string {
	t.Helper()
	rcp, err := agessh.ParseRecipient(authorized)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, rcp)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, token)
	w.Close()
	aw.Close()
	return buf.String()
}

func botComment(body string) ghComment {
	return ghComment{Body: body, User: ghAuthor{Login: requestAuthor, Type: "Bot"}}
}

// registration is the workflow's registration comment.
func registration(login string, keyID int64, expires time.Time, armored string) string {
	return fmt.Sprintf("%s\nrequester: %s\nssh-key-id: %d\nexpires-at: %s\n\nA runner registration token.\n\n```text\n%s```\n",
		runnerreq.RunnerRegistrationMarker, login, keyID, expires.Format(time.RFC3339), armored)
}

type requestFixture struct {
	addFixture
	req   *fakeRequest
	key   testKey
	home  string
	asset string
}

// useRequest is a host without runners for a write collaborator (not
// admin) with one ed25519 key on GitHub (id 11) and in ~/.ssh, answered at
// the second read with the host's token.
func useRequest(t *testing.T, f *fakeHost) requestFixture {
	t.Helper()
	if f.slots == nil {
		f.slots = map[string]*fakeSlot{}
	}
	if f.loggedIn == nil {
		f.loggedIn, f.volumes = map[string]bool{"claude": true}, map[string]bool{"claude": true, "nibfin-tools": true}
	}
	fx := useRunnerHost(t, f)
	home := t.TempDir()
	k := writeKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "ed25519", "")
	req := &fakeRequest{keys: []ghKey{{ID: 10, Key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOtherKeyOtherKeyOtherKeyOtherKeyOtherKe"}, {ID: 11, Key: k.authorized}}}
	armored := encryptTo(t, k.authorized, f.token)
	req.answer = func(n int) []ghComment {
		if n < 2 {
			return nil
		}
		return []ghComment{botComment(registration("Alice", 11, time.Now().Add(time.Hour), armored))}
	}
	fx.gh.req, fx.gh.perms = req, &repoPerms{Push: true}
	// GitHub's runner list needs admin: the request route never reads it.
	fx.gh.listErr = errors.New("ListRunners needs admin")
	fx.gh.downloads = nil

	asset := "actions-runner-linux-x64-2.338.2.tar.gz"
	rel := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.338.2", "assets": []map[string]any{
			{"name": "actions-runner-linux-arm64-2.338.2.tar.gz", "browser_download_url": "https://x.invalid/arm", "digest": "sha256:" + shaHex("arm")},
			{"name": asset, "browser_download_url": releaseArchiveURL, "digest": "sha256:" + shaHex(archive)},
		}})
	}))
	t.Cleanup(rel.Close)
	oldHome, oldPoll, oldWait, oldRel, oldPass := userHome, requestPoll, requestWait, runnerReleaseURL, readPassword
	t.Cleanup(func() {
		userHome, requestPoll, requestWait, runnerReleaseURL, readPassword = oldHome, oldPoll, oldWait, oldRel, oldPass
	})
	userHome = func() (string, error) { return home, nil }
	requestPoll, requestWait = time.Millisecond, 300*time.Millisecond
	runnerReleaseURL = rel.URL + "/repos/actions/runner/releases/latest"
	readPassword = func(*Terminal) ([]byte, error) { t.Error("asked for a passphrase"); return nil, errors.New("no") }
	return requestFixture{fx, req, k, home, asset}
}

// releaseArchiveURL is replaced per test by the archive server's URL.
var releaseArchiveURL string

// archiveURL points the release asset at the fixture's archive server.
func archiveURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, archive) }))
	t.Cleanup(srv.Close)
	old := releaseArchiveURL
	t.Cleanup(func() { releaseArchiveURL = old })
	releaseArchiveURL = srv.URL + "/runner.tar.gz"
}

func TestRunnerAddRequestRoute(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{}
	fx := useRequest(t, f)
	var events []string
	ctx := WithProgress(context.Background(), func(e Event) { events = append(events, e.Phase+" "+e.ID) })
	p, r, err := Run(ctx, RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (events %v)", err, f.events)
	}
	want := []string{"enable linger fake", "request registration github.com/cur/repo",
		"add runner claude-1", "add runner claude-2", "close request github.com/cur/repo"}
	if !reflect.DeepEqual(steps(p), want) {
		t.Fatalf("plan = %v", steps(p))
	}
	var keyStep Step
	for _, s := range p.Steps {
		if s.ID == "ssh-key" {
			keyStep = s
		}
	}
	if keyStep.Target != fx.key.pub || !keyStep.NoChange || !strings.HasPrefix(keyStep.Detail, "GitHub key 11 (SHA256:") {
		t.Errorf("key step = %+v", keyStep)
	}
	if d := p.Changes()[1].Detail; !strings.Contains(d, `"Nibfin runner request" for SSH key 11; waits up to`) {
		t.Errorf("request detail = %q", d)
	}
	if d := p.Changes()[2].Detail; d != "github.com/cur/repo, labels nibfin-42, .env NIBFIN_PROVIDER=claude" {
		t.Errorf("slot detail = %q", d)
	}
	wantEvents := []string{"linger", "request",
		"extract claude-1", "configure claude-1", "env claude-1 claude", "unit nibfin-runner-claude-1.service",
		"extract claude-2", "configure claude-2", "env claude-2 claude", "unit nibfin-runner-claude-2.service",
		"comment", "close completed"}
	if !reflect.DeepEqual(f.events, wantEvents) {
		t.Errorf("events = %v", f.events)
	}
	if fx.gh.tokens != 0 {
		t.Errorf("used the admin token endpoint %d times", fx.gh.tokens)
	}
	if len(fx.req.titles) != 1 || fx.req.titles[0] != runnerreq.RunnerRequestTitle ||
		fx.req.bodies[0] != runnerreq.RunnerRequestMarker+"\nssh-key-id: 11\n\n"+
			"gh nibfin runner add asks for a runner registration token for this repository, encrypted to this SSH key of the author's GitHub account.\n" {
		t.Errorf("request = %q %q", fx.req.titles, fx.req.bodies)
	}
	n1 := "my-host-01-" + shaHex("gh-nibfin runner\n0123abcd")[:8] + "-claude-1"
	n2 := strings.TrimSuffix(n1, "1") + "2"
	if !reflect.DeepEqual(fx.req.comments, []string{"Registered runners: " + n1 + ", " + n2}) || !reflect.DeepEqual(fx.req.closes, []string{"completed"}) {
		t.Errorf("comments %q closes %q", fx.req.comments, fx.req.closes)
	}
	for _, s := range f.shells {
		if s.script == "configure" {
			if l := flag(s.args, "--labels"); l != "nibfin-42" {
				t.Errorf("labels = %q", l)
			}
			for _, a := range s.args {
				if a == "--no-default-labels" || strings.Contains(a, f.token) {
					t.Errorf("configure args %q", s.args)
				}
			}
		}
	}
	for _, id := range []string{"request", "runner:claude-1", "request-close"} {
		if !contains(events, "started "+id) || !contains(events, "done "+id) {
			t.Errorf("progress of %s: %v", id, events)
		}
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), f.token) {
		t.Error("result has the token")
	}
	var got struct {
		Route, RequestIssue string
		Items               []Check
	}
	json.Unmarshal(b, &got)
	if got.Route != "request" || got.RequestIssue != "https://github.com/cur/repo/issues/7" {
		t.Errorf("result = %s", b)
	}
	for _, c := range got.Items {
		if !c.Confirmed && c.ID != "linger" {
			t.Errorf("item %s not confirmed: %s", c.ID, c.Reason)
		}
		if c.ID == "runner:claude-1" && (!strings.Contains(c.State, ".env NIBFIN_PROVIDER=claude") || !strings.Contains(c.State, "needs admin and was not read")) {
			t.Errorf("slot state = %q", c.State)
		}
	}
	if !hasItem(got.Items, "ssh-key") || !hasItem(got.Items, "request") || !hasItem(got.Items, "request-close") {
		t.Errorf("items = %+v", got.Items)
	}
}

func hasItem(items []Check, id string) bool {
	for _, c := range items {
		if c.ID == id {
			return true
		}
	}
	return false
}

func TestRunnerAddRouteChoice(t *testing.T) {
	// Neither admin nor push (also a repository the login cannot see; see
	// TestRepoPermissionsREST): refused before any write or request.
	f := &fakeHost{}
	fx := useRequest(t, f)
	fx.gh.perms = &repoPerms{}
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerRequestRefused || !strings.Contains(err.Error(), "write access to github.com/cur/repo") ||
		!strings.Contains(err.Error(), "collaborator") || len(f.events) != 0 || len(fx.req.bodies) != 0 {
		t.Errorf("read-only: %v, events %v", err, f.events)
	}

	// Admin: no SSH key, no request, labels nibfin and nibfin-<id>.
	f = &fakeHost{}
	fx = useRequest(t, f)
	fx.gh.perms = &repoPerms{Admin: true, Push: true}
	fx.gh.listErr = nil
	os.RemoveAll(filepath.Join(fx.home, ".ssh"))
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	if err != nil || !reflect.DeepEqual(steps(p), []string{"enable linger fake", "add runner claude-1"}) ||
		p.Changes()[1].Detail != "github.com/cur/repo, labels nibfin,nibfin-42, .env NIBFIN_PROVIDER=claude" {
		t.Errorf("admin plan = %v %+v, %v", steps(p), p.Changes(), err)
	}
}

func TestRunnerAddRequestNoKey(t *testing.T) {
	f := &fakeHost{}
	fx := useRequest(t, f)
	fx.req.keys = fx.req.keys[:1]
	writeKey(t, filepath.Join(fx.home, ".ssh"), "id_rsa", "rsa", "")
	_, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	msg := fmt.Sprint(err)
	if Code(err) != CodeSSHKeyMissing || !strings.Contains(msg, "id_ed25519.pub (ssh-ed25519)") || !strings.Contains(msg, "id_rsa.pub (ssh-rsa)") ||
		!strings.Contains(msg, "ssh-keygen -t ed25519") || !strings.Contains(msg, "gh ssh-key add ~/.ssh/id_ed25519.pub") ||
		!strings.Contains(msg, "gh auth refresh -s write:public_key") || !strings.Contains(msg, "run gh nibfin runner add again") {
		t.Errorf("err = %v", err)
	}
	// No key pair at all.
	os.RemoveAll(filepath.Join(fx.home, ".ssh"))
	if _, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background()); Code(err) != CodeSSHKeyMissing {
		t.Errorf("none: %v", err)
	}
	if len(f.events) != 0 || len(fx.req.bodies) != 0 {
		t.Errorf("wrote: %v", f.events)
	}
}

func TestFindSSHKeyPrefersEd25519(t *testing.T) {
	f := &fakeHost{}
	fx := useRequest(t, f)
	dir := filepath.Join(fx.home, ".ssh")
	rsaKey := writeKey(t, dir, "a_rsa", "rsa", "")
	ed2 := writeKey(t, dir, "b_ed25519", "ed25519", "")
	// A .pub without its private file is not considered.
	lone := writeKey(t, dir, "0_lone", "ed25519", "")
	os.Remove(lone.priv)
	fx.req.keys = []ghKey{{ID: 1, Key: rsaKey.authorized}, {ID: 2, Key: ed2.authorized}, {ID: 3, Key: lone.authorized}, {ID: 11, Key: fx.key.authorized}}
	k, err := findSSHKey(context.Background(), fx.gh, "alice")
	if err != nil || k.pubPath != ed2.pub || k.id != 2 {
		t.Errorf("key = %+v, %v", k, err)
	}
	fx.req.keys = []ghKey{{ID: 1, Key: rsaKey.authorized}}
	if k, err := findSSHKey(context.Background(), fx.gh, "alice"); err != nil || k.id != 1 || k.pub.Type() != ssh.KeyAlgoRSA {
		t.Errorf("rsa = %+v, %v", k, err)
	}
}

func TestRunnerAddRequestPassphrase(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{}
	fx := useRequest(t, f)
	k := writeKey(t, filepath.Join(fx.home, ".ssh"), "id_ed25519", "ed25519", "s3cret pass")
	fx.req.keys = []ghKey{{ID: 11, Key: k.authorized}}
	armored := encryptTo(t, k.authorized, f.token)
	fx.req.answer = func(int) []ghComment {
		return []ghComment{botComment(registration("alice", 11, time.Now().Add(time.Hour), armored))}
	}

	// Without a terminal the locked key stops before any write.
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeSSHKeyLocked || !strings.Contains(err.Error(), "in a terminal") || len(f.events) != 0 || len(fx.req.bodies) != 0 {
		t.Fatalf("no terminal: %v, events %v", err, f.events)
	}

	// Three wrong passphrases: locked, nothing written.
	prompts, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	// Close before TempDir's cleanup: Windows cannot remove an open file.
	t.Cleanup(func() { prompts.Close() })
	term := &Terminal{In: os.Stdin, Out: prompts, Err: prompts}
	tries := 0
	readPassword = func(*Terminal) ([]byte, error) { tries++; return []byte("wrong"), nil }
	_, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1, Terminal: term}), &yes{})
	if Code(err) != CodeSSHKeyLocked || tries != 3 || len(f.events) != 0 || len(fx.req.bodies) != 0 {
		t.Fatalf("wrong: %v, tries %d, events %v", err, tries, f.events)
	}

	// The right one on the second try registers.
	tries = 0
	readPassword = func(*Terminal) ([]byte, error) {
		tries++
		if tries == 1 {
			return []byte("nope"), nil
		}
		return []byte("s3cret pass"), nil
	}
	_, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1, Terminal: term}), &yes{})
	if err != nil || tries != 2 || !reflect.DeepEqual(fx.req.closes, []string{"completed"}) {
		t.Fatalf("right: %v, tries %d, closes %v, events %v", err, tries, fx.req.closes, f.events)
	}
	out, _ := os.ReadFile(prompts.Name())
	if strings.Count(string(out), "Enter the passphrase for "+k.priv) != 5 || strings.Contains(string(out), "s3cret") {
		t.Errorf("prompts = %q", out)
	}
}

func TestReadAnswerFilters(t *testing.T) {
	k := writeKey(t, t.TempDir(), "k", "ed25519", "")
	armored := encryptTo(t, k.authorized, "TOKEN")
	now := time.Now()
	good := registration("alice", 11, now.Add(time.Hour), armored)
	cases := map[string]ghComment{
		"wrong author": {Body: good, User: ghAuthor{Login: "mallory", Type: "User"}},
		"bot type":     {Body: good, User: ghAuthor{Login: requestAuthor, Type: "User"}},
		"requester":    botComment(registration("bob", 11, now.Add(time.Hour), armored)),
		"key id":       botComment(registration("alice", 12, now.Add(time.Hour), armored)),
		"expired":      botComment(registration("alice", 11, now.Add(-time.Minute), armored)),
		"two blocks":   botComment(registration("alice", 11, now.Add(time.Hour), armored+armored)),
		"no block":     botComment(registration("alice", 11, now.Add(time.Hour), "")),
		"no marker":    botComment(strings.TrimPrefix(good, runnerreq.RunnerRegistrationMarker+"\n")),
	}
	for name, c := range cases {
		if got, err := readAnswer([]ghComment{c}, "alice", 11, now); got != "" || err != nil {
			t.Errorf("%s: accepted %q, %v", name, got, err)
		}
	}
	got, err := readAnswer([]ghComment{cases["requester"], botComment(strings.ReplaceAll(good, "\n", "\r\n"))}, "ALICE", 11, now)
	if err != nil || got == "" {
		t.Fatalf("good: %q, %v", got, err)
	}
	id, _ := agessh.ParseIdentity(mustRead(t, k.priv))
	if tok, err := decryptToken(got, id); err != nil || tok != "TOKEN" {
		t.Errorf("decrypt = %q, %v", tok, err)
	}
	// Another key cannot decrypt it.
	other := writeKey(t, t.TempDir(), "o", "ed25519", "")
	oid, _ := agessh.ParseIdentity(mustRead(t, other.priv))
	if _, err := decryptToken(got, oid); Code(err) != CodeRunnerRequestFailed {
		t.Errorf("other key: %v", err)
	}
	// A refusal stops with its reason.
	_, err = readAnswer([]ghComment{botComment(runnerreq.RunnerRefusedMarker + "\nreason: alice has read permission\n\nAsk the owner.\n")}, "alice", 11, now)
	if Code(err) != CodeRunnerRequestRefused || !strings.Contains(err.Error(), "alice has read permission") {
		t.Errorf("refusal: %v", err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRunnerAddRequestStops(t *testing.T) {
	archiveURL(t)
	run := func(t *testing.T, setup func(requestFixture)) (requestFixture, *fakeHost, error) {
		f := &fakeHost{}
		fx := useRequest(t, f)
		setup(fx)
		_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		for _, e := range f.events {
			if strings.HasPrefix(e, "extract") || strings.HasPrefix(e, "configure") {
				t.Errorf("registered after a failed request: %v", f.events)
			}
		}
		return fx, f, err
	}
	t.Run("refused", func(t *testing.T) {
		fx, _, err := run(t, func(fx requestFixture) {
			fx.req.answer = func(int) []ghComment {
				return []ghComment{botComment(runnerreq.RunnerRefusedMarker + "\nreason: the workflow failed\n")}
			}
		})
		if Code(err) != CodeRunnerRequestRefused || !strings.Contains(err.Error(), "the workflow failed") || !reflect.DeepEqual(fx.req.closes, []string{"not_planned"}) {
			t.Errorf("%v, closes %v", err, fx.req.closes)
		}
	})
	t.Run("closed early", func(t *testing.T) {
		fx, f, err := run(t, func(fx requestFixture) { fx.req.answer, fx.req.closeAt = nil, 2 })
		if Code(err) != CodeRunnerRequestFailed || !strings.Contains(err.Error(), "closed without an answer") || len(fx.req.closes) != 0 || contains(f.events, "comment") {
			t.Errorf("%v, closes %v, events %v", err, fx.req.closes, f.events)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		start := time.Now()
		fx, _, err := run(t, func(fx requestFixture) { fx.req.answer = nil; requestWait = 50 * time.Millisecond })
		if Code(err) != CodeRunnerRequestTimeout || !strings.Contains(err.Error(), "can be run again") ||
			!reflect.DeepEqual(fx.req.closes, []string{"not_planned"}) || time.Since(start) > 5*time.Second {
			t.Errorf("%v, closes %v", err, fx.req.closes)
		}
	})
	t.Run("bad ciphertext", func(t *testing.T) {
		fx, _, err := run(t, func(fx requestFixture) {
			other := writeKey(t, t.TempDir(), "o", "ed25519", "")
			armored := encryptTo(t, other.authorized, "TOKEN")
			fx.req.answer = func(int) []ghComment {
				return []ghComment{botComment(registration("alice", 11, time.Now().Add(time.Hour), armored))}
			}
		})
		if Code(err) != CodeRunnerRequestFailed || !reflect.DeepEqual(fx.req.closes, []string{"not_planned"}) {
			t.Errorf("%v, closes %v", err, fx.req.closes)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		f := &fakeHost{}
		fx := useRequest(t, f)
		fx.req.answer = nil
		requestWait = time.Minute
		ctx, cancel := context.WithCancel(context.Background())
		fx.req.onRead = func(n int) {
			if n == 3 {
				cancel()
			}
		}
		_, _, err := Run(ctx, RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
		if Code(err) != CodeCancelled || !reflect.DeepEqual(fx.req.closes, []string{"not_planned"}) {
			t.Errorf("%v, closes %v", err, fx.req.closes)
		}
	})
}

func TestRunnerAddRequestRerunUnitsOnly(t *testing.T) {
	// A configured slot without its unit is finished without a request or
	// an SSH key; its .env is written before the unit.
	f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": {configured: true, name: "n1", url: "https://github.com/cur/repo"}}}
	fx := useRequest(t, f)
	os.RemoveAll(filepath.Join(fx.home, ".ssh"))
	p, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil || !reflect.DeepEqual(steps(p), []string{"enable linger fake", "add runner claude-1"}) {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	if !reflect.DeepEqual(f.events, []string{"linger", "env claude-1 claude", "unit nibfin-runner-claude-1.service"}) || len(fx.req.bodies) != 0 {
		t.Errorf("events = %v", f.events)
	}
	if r["route"] != "request" || r["requestIssue"] != nil {
		t.Errorf("result = %v", r)
	}
}

func TestRunnerAddRequestReadBackEnv(t *testing.T) {
	// A slot whose .env lacks the provider is not confirmed.
	f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": {configured: true, unit: true, enabled: true, active: true, name: "n1", url: "https://github.com/cur/repo"}}}
	useRequest(t, f)
	o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1})
	_, err := o.ReadBack(context.Background(), Plan{Steps: []Step{slotStep("claude-1")}})
	if Code(err) != CodeRunnerMissing {
		t.Errorf("err = %v", err)
	}
	f.slots["claude-1"].env = "claude"
	if _, err := o.ReadBack(context.Background(), Plan{Steps: []Step{slotStep("claude-1")}}); err != nil {
		t.Errorf("with .env: %v", err)
	}
}

func TestReleaseDownload(t *testing.T) {
	digest := "sha256:" + shaHex(archive)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials sent")
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v2.338.2", "assets": []map[string]any{
			{"name": "actions-runner-linux-x64-2.338.2.tar.gz", "browser_download_url": "https://h/x64", "digest": digest},
			{"name": "actions-runner-linux-arm64-2.338.2.tar.gz", "browser_download_url": "https://h/arm64", "digest": r.URL.Query().Get("arm")},
		}})
	}))
	defer srv.Close()
	oldC, oldU := httpClient, runnerReleaseURL
	t.Cleanup(func() { httpClient, runnerReleaseURL = oldC, oldU })
	httpClient = srv.Client()
	runnerReleaseURL = srv.URL
	d, err := releaseDownload(context.Background(), "x64")
	if err != nil || d.DownloadURL != "https://h/x64" || d.SHA256 != shaHex(archive) || d.Filename != "actions-runner-linux-x64-2.338.2.tar.gz" {
		t.Errorf("x64 = %+v, %v", d, err)
	}
	for _, bad := range []string{"", "sha256:abc", "sha1:" + shaHex("x")[:40], "sha256:" + strings.ToUpper(shaHex("x"))} {
		runnerReleaseURL = srv.URL + "?arm=" + bad
		if _, err := releaseDownload(context.Background(), "arm64"); Code(err) != CodeRunnerDownloadFailed {
			t.Errorf("digest %q: %v", bad, err)
		}
	}
	if _, err := releaseDownload(context.Background(), "riscv"); Code(err) != CodeRunnerDownloadFailed {
		t.Errorf("no asset: %v", err)
	}
}

// TestRepoPermissionsREST reads the gh login's permissions: a repository the
// login cannot see (GitHub answers 404) gives none, any other failure is an
// error.
func TestRepoPermissionsREST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/repos/o/r":
			io.WriteString(w, `{"permissions":{"admin":false,"push":true}}`)
		case "/api/v3/repos/o/secret":
			io.WriteString(w, `{"private":true,"permissions":{"admin":false,"push":true}}`)
		case "/api/v3/repos/o/hidden":
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found"}`)
		default:
			w.WriteHeader(http.StatusBadGateway)
			io.WriteString(w, `{"message":"Bad Gateway"}`)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c, err := api.NewRESTClient(api.ClientOptions{Host: "ghe.example", AuthToken: "x", Transport: rewrite{u}})
	if err != nil {
		t.Fatal(err)
	}
	g := restGitHub{c}
	ctx := context.Background()
	if p, err := g.RepoPermissions(ctx, "ghe.example/o/r"); err != nil || p != (repoPerms{Push: true}) {
		t.Errorf("write: %+v, %v", p, err)
	}
	if p, err := g.RepoPermissions(ctx, "ghe.example/o/secret"); err != nil || p != (repoPerms{Push: true, Private: true}) {
		t.Errorf("private: %+v, %v", p, err)
	}
	if p, err := g.RepoPermissions(ctx, "ghe.example/o/hidden"); err != nil || p != (repoPerms{}) {
		t.Errorf("hidden: %+v, %v", p, err)
	}
	if _, err := g.RepoPermissions(ctx, "ghe.example/o/down"); Code(err) != CodeGitHubAPIFailed || !strings.Contains(err.Error(), "502") {
		t.Errorf("502: %v", err)
	}
}

// deployKeyAt is the fake host's deploy key path for github.com/cur/repo.
const deployKeyAt = "/home/u/.local/share/nibfin/deploy-keys/github.com/cur/repo/id_ed25519"

// usePrivateRequest is useRequest on a private repository; the write
// script's stdin (the private key) is saved to the returned file.
func usePrivateRequest(t *testing.T, f *fakeHost) (requestFixture, string) {
	t.Helper()
	fx := useRequest(t, f)
	fx.gh.perms = &repoPerms{Push: true, Private: true}
	f.deployPriv = filepath.Join(t.TempDir(), "deploy-priv")
	return fx, f.deployPriv
}

func shellNames(f *fakeHost) []string {
	var out []string
	for _, s := range f.shells {
		out = append(out, s.script)
	}
	return out
}

func TestRunnerAddRequestDeployKey(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{}
	fx, privFile := usePrivateRequest(t, f)
	p, r, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatalf("Run: %v (events %v)", err, f.events)
	}
	want := []string{"enable linger fake", "create deploy key " + deployKeyAt, "request registration github.com/cur/repo",
		"add runner claude-1", "close request github.com/cur/repo"}
	if !reflect.DeepEqual(steps(p), want) {
		t.Fatalf("plan = %v", steps(p))
	}
	if d := p.Changes()[2].Detail; !strings.Contains(d, "for SSH key 11 with read-only deploy key;") {
		t.Errorf("request detail = %q", d)
	}
	// The key is written before the request is opened.
	if len(f.events) < 3 || !reflect.DeepEqual(f.events[:3], []string{"linger", "deploy-key " + deployKeyAt, "request"}) {
		t.Errorf("events = %v", f.events)
	}
	// The private key reached the host only on stdin, and the public key
	// in the request is its own.
	pemBytes := mustRead(t, privFile)
	priv, err := ssh.ParseRawPrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("private key on stdin: %v", err)
	}
	signer, _ := ssh.NewSignerFromKey(priv)
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Errorf("key type %s", signer.PublicKey().Type())
	}
	for _, s := range f.shells {
		for _, a := range s.args {
			if strings.Contains(a, "PRIVATE KEY") {
				t.Errorf("%s has the private key in its arguments", s.script)
			}
		}
		if s.script == "deploy-write" && !reflect.DeepEqual(s.args, []string{deployKeyAt, line + " nibfin-deploy-key"}) {
			t.Errorf("write args = %q", s.args)
		}
	}
	wantBody := runnerreq.RunnerRequestMarker + "\nssh-key-id: 11\ndeploy-key: " + line + "\n\n"
	if len(fx.req.bodies) != 1 || !strings.HasPrefix(fx.req.bodies[0], wantBody) {
		t.Errorf("body = %q, want prefix %q", fx.req.bodies, wantBody)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "PRIVATE") {
		t.Error("the result has the private key")
	}
	var got struct {
		DeployKey string
		Items     []Check
	}
	json.Unmarshal(b, &got)
	if got.DeployKey != deployKeyAt {
		t.Errorf("result deployKey = %q", got.DeployKey)
	}
	found := false
	for _, c := range got.Items {
		if c.ID == "deploy-key" {
			found = true
			if !c.Confirmed || !strings.Contains(c.State, "mode 0600") || !strings.Contains(c.State, ssh.FingerprintSHA256(signer.PublicKey())) {
				t.Errorf("deploy key item = %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("no deploy key item: %+v", got.Items)
	}

	// A later request from the same host reuses the key.
	f.slots = map[string]*fakeSlot{}
	f.shells, f.events = nil, nil
	os.Remove(privFile)
	p, _, err = Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if !contains(allSteps(p), "use deploy key "+deployKeyAt) || contains(steps(p), "create deploy key "+deployKeyAt) {
		t.Errorf("rerun plan = %v", allSteps(p))
	}
	if contains(shellNames(f), "deploy-write") {
		t.Errorf("rerun rewrote the key: %v", shellNames(f))
	}
	if len(fx.req.bodies) != 2 || !strings.HasPrefix(fx.req.bodies[1], wantBody) {
		t.Errorf("rerun body = %q", fx.req.bodies[1:])
	}
}

func TestRunnerAddDeployKeyOnlyPrivateRequest(t *testing.T) {
	archiveURL(t)
	// A public repository: no deploy key read, written or sent.
	f := &fakeHost{}
	fx := useRequest(t, f)
	if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); err != nil {
		t.Fatal(err)
	}
	for _, n := range shellNames(f) {
		if strings.HasPrefix(n, "deploy-") {
			t.Errorf("public repository ran %s", n)
		}
	}
	if strings.Contains(fx.req.bodies[0], "deploy-key") {
		t.Errorf("public body = %q", fx.req.bodies[0])
	}

	// The admin route of a private repository: no deploy key either.
	f = &fakeHost{}
	fx = useRequest(t, f)
	fx.gh.perms, fx.gh.listErr = &repoPerms{Admin: true, Push: true, Private: true}, nil
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).Plan(context.Background())
	if err != nil || !reflect.DeepEqual(steps(p), []string{"enable linger fake", "add runner claude-1"}) {
		t.Errorf("admin: %v, %v", allSteps(p), err)
	}
	for _, n := range shellNames(f) {
		if strings.HasPrefix(n, "deploy-") {
			t.Errorf("admin route ran %s", n)
		}
	}

	// A rerun that only finishes units opens no request and makes no key.
	f = &fakeHost{slots: map[string]*fakeSlot{"claude-1": {configured: true, name: "n1", url: "https://github.com/cur/repo"}}}
	usePrivateRequest(t, f)
	if _, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{}); err != nil {
		t.Fatal(err)
	}
	for _, n := range shellNames(f) {
		if strings.HasPrefix(n, "deploy-") {
			t.Errorf("units-only rerun ran %s", n)
		}
	}
}

func TestRunnerAddDeployKeyWriteFails(t *testing.T) {
	archiveURL(t)
	f := &fakeHost{shellExit: map[string]int{"deploy-write": 1}, shellErr: map[string]string{"deploy-write": "mkdir: Permission denied"}}
	fx, _ := usePrivateRequest(t, f)
	_, _, err := Run(context.Background(), RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}), &yes{})
	if Code(err) != CodeRunnerInstallFailed || !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("err = %v", err)
	}
	if len(fx.req.bodies) != 0 || contains(f.events, "request") {
		t.Errorf("a request was opened: %v", f.events)
	}
}

func TestRunnerAddDeployKeyReadBack(t *testing.T) {
	good := writeKey(t, t.TempDir(), "k", "ed25519", "")
	other := writeKey(t, t.TempDir(), "o", "ed25519", "")
	rsaKey := writeKey(t, t.TempDir(), "r", "rsa", "")
	cases := []struct {
		name, pub, mode, sent string
		ok                    bool
		state                 string
	}{
		{name: "good", pub: good.authorized + " nibfin-deploy-key\n", mode: "600", ok: true, state: "mode 0600"},
		{name: "sent", pub: good.authorized + " nibfin-deploy-key\n", mode: "600", sent: good.authorized, ok: true, state: "mode 0600"},
		{name: "absent", state: "absent"},
		{name: "mode", pub: good.authorized + "\n", mode: "644", state: "mode 0644"},
		{name: "mismatch", pub: good.authorized + "\n", mode: "600", sent: other.authorized, state: "differs"},
		{name: "rsa", pub: rsaKey.authorized + "\n", mode: "600", state: "not ed25519"},
	}
	for _, c := range cases {
		f := &fakeHost{slots: map[string]*fakeSlot{"claude-1": {configured: true, unit: true, enabled: true, active: true, name: "n1", url: "https://github.com/cur/repo", env: "claude"}}}
		fx, _ := usePrivateRequest(t, f)
		fx.gh.listErr = errors.New("ListRunners must not be called")
		f.deployPub, f.deployMode = c.pub, c.mode
		o := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 1}).(*runnerAdd)
		o.deploySent = c.sent
		if c.sent != "" {
			o.deployPath = deployKeyAt
		}
		plan := Plan{Steps: []Step{deployKeyStep(deployKeyAt, nil), requestStep("github.com/cur/repo", 11, true), slotStep("claude-1")}}
		r, _ := o.ReadBack(context.Background(), plan)
		var item *Check
		for _, x := range r[ResultItems].([]Check) {
			if x.ID == "deploy-key" {
				item = &x
			}
		}
		if item == nil || item.Confirmed != c.ok || !strings.Contains(item.State, c.state) {
			t.Errorf("%s: item %+v", c.name, item)
		}
		if r["deployKey"] != deployKeyAt {
			t.Errorf("%s: deployKey = %v", c.name, r["deployKey"])
		}
		if !contains(shellNames(f), "deploy-check") {
			t.Errorf("%s: not read from the host", c.name)
		}
	}
}

func TestRequestBodyDeployKey(t *testing.T) {
	if b := requestBody(11, ""); strings.Contains(b, "deploy-key") {
		t.Errorf("no key: %q", b)
	}
	if b := requestBody(11, "ssh-ed25519 AAAA"); !strings.HasPrefix(b, runnerreq.RunnerRequestMarker+"\nssh-key-id: 11\ndeploy-key: ssh-ed25519 AAAA\n\n") {
		t.Errorf("key: %q", b)
	}
}
