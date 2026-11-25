package op

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/armor"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"github.com/taco3064/gh-nibfin/internal/runnerreq"
)

// The request route of runner add: a write collaborator of the execution
// repository gets the registration token through a request Issue that
// nibfin-runner-request.yml answers with the token encrypted to one of the
// user's GitHub SSH keys (internal/runnerreq holds the contract).

const (
	routeAdmin   = "admin"
	routeRequest = "request"

	actUseSSHKey           = "use SSH key"
	actRequestRegistration = "request registration"
	actCloseRequest        = "close request"

	// requestAuthor is the workflow's GITHUB_TOKEN identity, the only
	// author whose answer counts.
	requestAuthor = "github-actions[bot]"
	// passphraseTries is how often a wrong passphrase may be entered.
	passphraseTries = 3
)

var (
	// userHome is the home directory whose .ssh holds the keys; tests
	// replace it.
	userHome = os.UserHomeDir
	// requestPoll and requestWait pace and bound the wait for the answer.
	requestPoll = 5 * time.Second
	requestWait = 10 * time.Minute
	// closeTimeout bounds closing the request after a cancel.
	closeTimeout = 15 * time.Second
	// readPassword reads a passphrase from the terminal without echo; tests
	// replace it.
	readPassword = func(t *Terminal) ([]byte, error) { return term.ReadPassword(int(t.In.Fd())) }
	// timeNow is the clock the answer's expiry is checked against.
	timeNow = time.Now
)

// localKey is a local SSH key pair whose public key is on the user's GitHub
// account under ID.
type localKey struct {
	pubPath, privPath string
	pub               ssh.PublicKey
	id                int64
}

func (k *localKey) fingerprint() string { return ssh.FingerprintSHA256(k.pub) }

func sshKeyStep(k *localKey) Step {
	return Step{ID: "ssh-key", Action: actUseSSHKey, Target: k.pubPath,
		Detail: fmt.Sprintf("GitHub key %d (%s)", k.id, k.fingerprint()), Current: "matches", Next: "matches", NoChange: true}
}

func requestStep(repo string, keyID int64) Step {
	return Step{ID: "request", Action: actRequestRegistration, Target: repo,
		Detail: fmt.Sprintf("Issue %q for SSH key %d; waits up to %s", runnerreq.RunnerRequestTitle, keyID, waitText(requestWait)),
		Next:   "token received"}
}

func requestCloseStep(repo string) Step {
	return Step{ID: "request-close", Action: actCloseRequest, Target: repo, Next: "commented and closed"}
}

func waitText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	}
	return d.String()
}

// sshKeyHelp is the guidance when no local key matches the GitHub account.
const sshKeyHelp = "create a key with: ssh-keygen -t ed25519, add it to your GitHub account with: " +
	"gh ssh-key add ~/.ssh/id_ed25519.pub (run gh auth refresh -s write:public_key when gh asks for that scope), " +
	"then run gh nibfin runner add again"

// findSSHKey returns the local key pair in ~/.ssh whose public key (ssh-ed25519
// or ssh-rsa) is one of login's GitHub keys, preferring ed25519, then the
// file name. It only reads.
func findSSHKey(ctx context.Context, gh github, login string) (*localKey, error) {
	home, err := userHome()
	if err != nil {
		return nil, Errorf(CodeSSHKeyMissing, "cannot find the home directory for ~/.ssh: %v; %s", err, sshKeyHelp)
	}
	pubs, _ := filepath.Glob(filepath.Join(home, ".ssh", "*.pub"))
	sort.Strings(pubs)
	var local []*localKey
	var considered []string
	for _, p := range pubs {
		priv := strings.TrimSuffix(p, ".pub")
		if fi, err := os.Stat(priv); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey(b)
		if err != nil || (pub.Type() != ssh.KeyAlgoED25519 && pub.Type() != ssh.KeyAlgoRSA) {
			continue
		}
		local = append(local, &localKey{pubPath: p, privPath: priv, pub: pub})
		considered = append(considered, p+" ("+pub.Type()+")")
	}
	if len(local) == 0 {
		return nil, Errorf(CodeSSHKeyMissing, "no ssh-ed25519 or ssh-rsa key pair in %s matches a key of %s on GitHub: none found; %s",
			filepath.Join(home, ".ssh"), login, sshKeyHelp)
	}
	keys, err := gh.UserKeys(ctx, login)
	if err != nil {
		return nil, err
	}
	var match []*localKey
	for _, k := range local {
		for _, g := range keys {
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(g.Key))
			if err == nil && pub.Type() == k.pub.Type() && bytes.Equal(pub.Marshal(), k.pub.Marshal()) {
				k.id = g.ID
				match = append(match, k)
				break
			}
		}
	}
	if len(match) == 0 {
		return nil, Errorf(CodeSSHKeyMissing, "no ssh-ed25519 or ssh-rsa key pair in %s matches a key of %s on GitHub (considered %s); %s",
			filepath.Join(home, ".ssh"), login, strings.Join(considered, ", "), sshKeyHelp)
	}
	sort.SliceStable(match, func(i, j int) bool {
		ei, ej := match[i].pub.Type() == ssh.KeyAlgoED25519, match[j].pub.Type() == ssh.KeyAlgoED25519
		if ei != ej {
			return ei
		}
		return match[i].pubPath < match[j].pubPath
	})
	return match[0], nil
}

// unlockSSHKey reads the private key and returns its age identity. A key
// with a passphrase is unlocked on the terminal, without echo, in at most
// three tries; without a terminal it is ssh_key_locked. The private key and
// passphrase stay in this process.
func unlockSSHKey(k *localKey, t *Terminal) (age.Identity, error) {
	pem, err := os.ReadFile(k.privPath)
	if err != nil {
		return nil, Errorf(CodeSSHKeyMissing, "cannot read the private key %s: %v", k.privPath, err)
	}
	matches := func(raw any) error {
		s, err := ssh.NewSignerFromKey(raw)
		if err != nil {
			return Errorf(CodeSSHKeyMissing, "cannot use the private key %s: %v", k.privPath, err)
		}
		if !bytes.Equal(s.PublicKey().Marshal(), k.pub.Marshal()) {
			return Errorf(CodeSSHKeyMissing, "the private key %s does not belong to %s", k.privPath, k.pubPath)
		}
		return nil
	}
	raw, err := ssh.ParseRawPrivateKey(pem)
	if err == nil {
		if err := matches(raw); err != nil {
			return nil, err
		}
		id, err := agessh.ParseIdentity(pem)
		if err != nil {
			return nil, Errorf(CodeSSHKeyMissing, "cannot use the private key %s: %v", k.privPath, err)
		}
		return id, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, Errorf(CodeSSHKeyMissing, "cannot read the private key %s: %v", k.privPath, err)
	}
	if t == nil || t.Err == nil || t.In == nil {
		return nil, Errorf(CodeSSHKeyLocked, "the SSH key %s has a passphrase and no terminal can ask for it; run gh nibfin runner add in a terminal", k.privPath)
	}
	for try := 0; try < passphraseTries; try++ {
		fmt.Fprintf(t.Err, "Enter the passphrase for %s: ", k.privPath)
		pass, err := readPassword(t)
		fmt.Fprintln(t.Err)
		if err != nil {
			return nil, Errorf(CodeSSHKeyLocked, "cannot read the passphrase of %s: %v; run gh nibfin runner add in a terminal", k.privPath, err)
		}
		raw, err := ssh.ParseRawPrivateKeyWithPassphrase(pem, pass)
		if err != nil {
			continue
		}
		if err := matches(raw); err != nil {
			return nil, err
		}
		id, err := agessh.NewEncryptedSSHIdentity(k.pub, pem, func() ([]byte, error) { return pass, nil })
		if err != nil {
			return nil, Errorf(CodeSSHKeyMissing, "cannot use the private key %s: %v", k.privPath, err)
		}
		return id, nil
	}
	return nil, Errorf(CodeSSHKeyLocked, "the passphrase of %s was wrong %d times; run gh nibfin runner add again in a terminal", k.privPath, passphraseTries)
}

// runnerRequest is one request Issue and what became of it.
type runnerRequest struct {
	issue    ghIssue
	answered bool
	// closed is true once the Issue is known to be closed, by this process
	// or by the workflow.
	closed bool
	// closeErr is the failure of the close item, for read-back.
	closeErr error
}

// requestBody is the request Issue's body.
func requestBody(keyID int64) string {
	return runnerreq.RunnerRequestMarker + "\n" +
		runnerreq.RunnerKeyIDField + ": " + strconv.FormatInt(keyID, 10) + "\n\n" +
		"gh nibfin runner add asks for a runner registration token for this repository, encrypted to this SSH key of the author's GitHub account.\n"
}

// requestToken opens the request Issue as the request item, waits for the
// workflow's answer and decrypts the token with id.
func (a *runnerAdd) requestToken(ctx context.Context, env *addEnv, id age.Identity) (string, error) {
	var token string
	err := Track(ctx, requestStep(env.repo, env.key.id), func() error {
		is, err := env.gh.CreateIssue(ctx, env.repo, runnerreq.RunnerRequestTitle, requestBody(env.key.id))
		if err != nil {
			return err
		}
		a.req = &runnerRequest{issue: is}
		armored, err := a.waitAnswer(ctx, env)
		if err != nil {
			return err
		}
		if token, err = decryptToken(armored, id); err != nil {
			return err
		}
		a.req.answered = true
		return nil
	})
	return token, err
}

// waitAnswer polls the request's comments and state until the workflow
// answers, refuses, the Issue closes or requestWait passes.
func (a *runnerAdd) waitAnswer(ctx context.Context, env *addEnv) (string, error) {
	is := a.req.issue
	where := fmt.Sprintf("%s#%d", env.repo, is.Number)
	deadline := time.Now().Add(requestWait)
	read := func() (string, error) {
		cs, err := env.gh.IssueComments(ctx, env.repo, is.Number)
		if err != nil {
			return "", cancelled(ctx, err)
		}
		return readAnswer(cs, env.user.Login, env.key.id, timeNow())
	}
	for {
		armored, err := read()
		if err != nil || armored != "" {
			return armored, err
		}
		st, err := env.gh.Issue(ctx, env.repo, is.Number)
		if err != nil {
			return "", cancelled(ctx, err)
		}
		if st.State == "closed" {
			a.req.closed = true
			// The workflow comments before it closes; read once more.
			if armored, err := read(); err != nil || armored != "" {
				return armored, err
			}
			return "", Errorf(CodeRunnerRequestFailed, "the runner request %s was closed without an answer", where)
		}
		if !time.Now().Before(deadline) {
			return "", Errorf(CodeRunnerRequestTimeout, "the runner request %s got no answer within %s; it is closed as not planned, and gh nibfin runner add can be run again",
				where, waitText(requestWait))
		}
		select {
		case <-ctx.Done():
			return "", Errorf(CodeCancelled, "cancelled")
		case <-time.After(requestPoll):
		}
	}
}

// cancelled reports err as cancelled when ctx is done.
func cancelled(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return Errorf(CodeCancelled, "cancelled")
	}
	return err
}

// readAnswer returns the armored token of the workflow's registration
// comment for login and keyID, or "" when there is none yet. A refusal is
// runner_request_refused with its reason. Comments by anyone else, for
// another requester or key, expired or without exactly one armored block
// are ignored.
func readAnswer(cs []ghComment, login string, keyID int64, now time.Time) (string, error) {
	for _, c := range cs {
		if c.User.Login != requestAuthor || c.User.Type != "Bot" {
			continue
		}
		body := strings.ReplaceAll(c.Body, "\r\n", "\n")
		first, rest, _ := strings.Cut(body, "\n")
		switch strings.TrimSpace(first) {
		case runnerreq.RunnerRefusedMarker:
			reason := field(rest, runnerreq.RunnerReasonField)
			if reason == "" {
				reason = "no reason given"
			}
			return "", Errorf(CodeRunnerRequestRefused, "the runner request was refused: %s", reason)
		case runnerreq.RunnerRegistrationMarker:
			if !strings.EqualFold(field(rest, runnerreq.RunnerRequesterField), login) ||
				field(rest, runnerreq.RunnerKeyIDField) != strconv.FormatInt(keyID, 10) {
				continue
			}
			exp, err := time.Parse(time.RFC3339, field(rest, runnerreq.RunnerExpiresField))
			if err != nil || !exp.After(now) {
				continue
			}
			if armored, ok := armoredBlock(rest); ok {
				return armored, nil
			}
		}
	}
	return "", nil
}

// field returns the value of the first "name: value" line of s.
func field(s, name string) string {
	for _, l := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(l, name+":"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// armoredBlock returns the one armored age block of s.
func armoredBlock(s string) (string, bool) {
	if strings.Count(s, runnerreq.RunnerTokenBegin) != 1 || strings.Count(s, runnerreq.RunnerTokenEnd) != 1 {
		return "", false
	}
	b := strings.Index(s, runnerreq.RunnerTokenBegin)
	e := strings.Index(s, runnerreq.RunnerTokenEnd)
	if e < b {
		return "", false
	}
	return s[b:e+len(runnerreq.RunnerTokenEnd)] + "\n", true
}

var tokenRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,512}$`)

// decryptToken decrypts the armored answer to the one-line token.
func decryptToken(armored string, id age.Identity) (string, error) {
	r, err := age.Decrypt(armor.NewReader(strings.NewReader(armored)), id)
	if err != nil {
		return "", Errorf(CodeRunnerRequestFailed, "cannot decrypt the runner registration token: %v", err)
	}
	b, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil {
		return "", Errorf(CodeRunnerRequestFailed, "cannot decrypt the runner registration token: %v", err)
	}
	token := strings.TrimRight(string(b), "\r\n")
	if !tokenRE.MatchString(token) {
		return "", Errorf(CodeRunnerRequestFailed, "the decrypted runner registration token is not one token line")
	}
	return token, nil
}

// finishRequest is the close item: after registration it comments the
// runner names and closes the Issue as completed; after any failure it
// closes it as not planned, with a fresh bounded context when ctx is done.
// The failure err stays the result; a close failure on success is returned.
func (a *runnerAdd) finishRequest(ctx context.Context, env *addEnv, err error) error {
	r := a.req
	if r == nil {
		return err
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
	step := requestCloseStep(env.repo)
	cerr := Track(cctx, step, func() error {
		if err == nil {
			if e := env.gh.CreateComment(cctx, env.repo, r.issue.Number, "Registered runners: "+strings.Join(a.registered, ", ")); e != nil {
				return e
			}
		}
		if r.closed {
			return nil
		}
		reason := "completed"
		if err != nil {
			reason = "not_planned"
		}
		if e := env.gh.CloseIssue(cctx, env.repo, r.issue.Number, reason); e != nil {
			return e
		}
		r.closed = true
		return nil
	})
	r.closeErr = cerr
	if err != nil {
		return err
	}
	return cerr
}

// readBackRequest reads the request items back: the SSH key still matches
// the GitHub account, the token was received and the Issue is closed. They
// are read when the plan has them or Apply opened a request.
func (a *runnerAdd) readBackRequest(ctx context.Context, gh github, repo string, plan Plan, checks *Checks, r Result) error {
	var keyStep *Step
	for _, s := range plan.Steps {
		if s.Action == actUseSSHKey {
			keyStep = &s
		}
	}
	_, planned := findStep(plan.Steps, actRequestRegistration)
	if !planned && a.req == nil {
		return nil
	}
	reqStep, closeStep := requestStep(repo, 0), requestCloseStep(repo)
	if keyStep != nil {
		checks.Expect(*keyStep)
	}
	checks.Expect(reqStep, closeStep)
	if keyStep != nil {
		user, err := gh.User(ctx)
		if err != nil {
			return err
		}
		k, err := findSSHKey(ctx, gh, user.Login)
		switch {
		case err != nil:
			checks.Set(*keyStep, false, "no match", AsError(err).Error())
		case k.pubPath != keyStep.Target:
			checks.Set(*keyStep, false, "matches "+k.pubPath, "another key matches first")
		default:
			checks.Set(*keyStep, true, "matches", "")
		}
	}
	if a.req == nil {
		checks.Set(reqStep, false, "not opened", "no request was opened")
		checks.Set(closeStep, false, "not opened", "no request was opened")
		return nil
	}
	r["requestIssue"] = a.req.issue.HTMLURL
	if a.req.answered {
		checks.Set(reqStep, true, "token received on "+a.req.issue.HTMLURL, "")
	} else {
		checks.Set(reqStep, false, "no token", "no registration token was received")
	}
	is, err := gh.Issue(ctx, repo, a.req.issue.Number)
	if err != nil {
		return err
	}
	why := "the request Issue is " + is.State
	if a.req.closeErr != nil {
		why += ": " + AsError(a.req.closeErr).Error()
	}
	checks.Set(closeStep, is.State == "closed", is.State, why)
	return nil
}
