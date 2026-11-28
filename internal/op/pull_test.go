package op

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
)

const eofErr = "Trying to pull ghcr.io/taco3064/nibfin:main...\nCopying blob 1234\nError: failed to decode message from stream: unexpected EOF\n"

const eofRetry = "nibfin: podman pull " + LoginImage + " failed (Error: failed to decode message from stream: unexpected EOF); retry 1 of 3\n"

// stubPullRetry makes pull retries record their waits instead of sleeping
// and collects their lines.
func stubPullRetry(t *testing.T) (*strings.Builder, *[]time.Duration) {
	t.Helper()
	oldSleep, oldLog := ghretry.Sleep, ghretry.Log
	var log strings.Builder
	var waits []time.Duration
	ghretry.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	ghretry.Log = &log
	t.Cleanup(func() { ghretry.Sleep, ghretry.Log = oldSleep, oldLog })
	return &log, &waits
}

// podmanVerbs names each podman call by its first two arguments.
func podmanVerbs(calls [][]string) []string {
	out := []string{}
	for _, c := range calls {
		out = append(out, strings.Join(c[:2], " "))
	}
	return out
}

func TestCheckPullRetriesOnce(t *testing.T) {
	log, waits := stubPullRetry(t)
	claude, _ := lookupProvider("claude")
	f := &fakeHost{loggedIn: map[string]bool{"claude": true}, pullExits: []int{125}, pullErr: eofErr}
	l := &login{host: f}
	if in, err := l.check(context.Background(), claude); !in || err != nil {
		t.Fatalf("check = %v, %v", in, err)
	}
	if got := podmanVerbs(f.calls); !reflect.DeepEqual(got, []string{"pull " + LoginImage, "pull " + LoginImage, "run --rm"}) {
		t.Errorf("calls = %q", got)
	}
	if log.String() != eofRetry {
		t.Errorf("log = %q\nwant %q", log.String(), eofRetry)
	}
	if !reflect.DeepEqual(*waits, []time.Duration{time.Second}) {
		t.Errorf("waits = %v", *waits)
	}
	if run := f.calls[2]; !hasArg(run, "--pull=never") || hasArg(run, "--pull=newer") {
		t.Errorf("run = %q", run)
	}
}

func TestCheckPullKeepsFailing(t *testing.T) {
	log, waits := stubPullRetry(t)
	claude, _ := lookupProvider("claude")
	f := &fakeHost{loggedIn: map[string]bool{"claude": true}, pullExits: []int{125, 125, 125, 125}, pullErr: eofErr, imageExit: 1}
	l := &login{host: f}
	_, err := l.check(context.Background(), claude)
	if Code(err) != CodePodmanFailed {
		t.Fatalf("err = %v", err)
	}
	for _, s := range []string{"podman could not pull " + LoginImage + " for the claude login check after 4 tries", "Copying blob 1234", "unexpected EOF"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("err %q lacks %q", err, s)
		}
	}
	p := "pull " + LoginImage
	if got := podmanVerbs(f.calls); !reflect.DeepEqual(got, []string{p, p, p, p, "image exists"}) {
		t.Errorf("calls = %q", got)
	}
	if !reflect.DeepEqual(f.calls[4], []string{"image", "exists", LoginImage}) {
		t.Errorf("image exists = %q", f.calls[4])
	}
	if !reflect.DeepEqual(*waits, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Errorf("waits = %v", *waits)
	}
	if n := strings.Count(log.String(), "\n"); n != 3 || !strings.Contains(log.String(), "retry 3 of 3") {
		t.Errorf("log = %q", log.String())
	}
}

func TestCheckPullFallsBackToTheLocalImage(t *testing.T) {
	log, _ := stubPullRetry(t)
	claude, _ := lookupProvider("claude")
	f := &fakeHost{loggedIn: map[string]bool{"claude": true}, pullExits: []int{125, 125, 125, 125}, pullErr: eofErr}
	l := &login{host: f}
	if in, err := l.check(context.Background(), claude); !in || err != nil {
		t.Fatalf("check = %v, %v", in, err)
	}
	if got := podmanVerbs(f.calls); len(got) != 6 || got[4] != "image exists" || got[5] != "run --rm" || !hasArg(f.calls[5], "--pull=never") {
		t.Errorf("calls = %q", got)
	}
	if want := "nibfin: podman pull " + LoginImage + " failed after 4 tries (Error: failed to decode message from stream: unexpected EOF); using the local image\n"; !strings.HasSuffix(log.String(), want) {
		t.Errorf("log = %q", log.String())
	}
}

// A container that started and failed is not run again: only the pull is
// retried.
func TestRunFailureAfterPullIsNotRetried(t *testing.T) {
	log, _ := stubPullRetry(t)
	codex, _ := lookupProvider("codex")
	for exit, want := range map[int]string{125: CodePodmanFailed, 7: CodeLoginCheckFailed} {
		f := &fakeHost{checkExit: exit}
		if _, err := (&login{host: f}).check(context.Background(), codex); Code(err) != want || !reflect.DeepEqual(podmanVerbs(f.calls), []string{"pull " + LoginImage, "run --rm"}) {
			t.Errorf("check exit %d: %v, calls %q", exit, err, f.calls)
		}
	}
	for exit, want := range map[int]string{1: CodeLoginFailed, 125: CodePodmanFailed} {
		f := &fakeHost{loginExit: exit}
		useHost(t, f)
		in := &recInteract{}
		if err := (&login{host: f, p: LoginParams{Interact: in}}).runLogin(context.Background(), codex); Code(err) != want || !reflect.DeepEqual(podmanVerbs(f.calls), []string{"pull " + LoginImage, "run --rm"}) {
			t.Errorf("login exit %d: %v, calls %q", exit, err, f.calls)
		}
	}
	if log.Len() != 0 {
		t.Errorf("log = %q", log.String())
	}
}

func TestLoginPullFailureEndsTheLogin(t *testing.T) {
	stubPullRetry(t)
	codex, _ := lookupProvider("codex")
	f := &fakeHost{pullExits: []int{1, 1, 1, 1}, pullErr: "Error: denied\n", imageExit: 1}
	in := &recInteract{}
	err := (&login{host: f, p: LoginParams{Interact: in}}).runLogin(context.Background(), codex)
	if Code(err) != CodePodmanFailed || !strings.Contains(err.Error(), "for the codex login after 4 tries: Error: denied") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(kinds(in.steps), []string{StepFailed}) || in.steps[0].Message != err.Error() {
		t.Errorf("steps = %+v", in.steps)
	}
	for _, c := range f.calls {
		if c[0] == "run" {
			t.Errorf("login ran after a failed pull: %q", c)
		}
	}
}

// runner add's login check pulls the image with retries and then runs the
// check without a pull of its own.
func TestRunnerAddLoginCheckRetriesThePull(t *testing.T) {
	log, _ := stubPullRetry(t)
	f := &fakeHost{probe: strings.Replace(defaultProbe, "linger 0", "linger 1", 1),
		volumes: map[string]bool{"nibfin-tools": true, "claude": true}, loggedIn: map[string]bool{"claude": true},
		pullExits: []int{125}, pullErr: eofErr}
	f.slots = map[string]*fakeSlot{"claude-1": {configured: true, unit: true, enabled: true, name: "a", url: "https://ghe.example/o/r"}}
	useRunnerHost(t, f)
	p, err := RunnerAdd(RunnerAddParams{Provider: "claude", Count: 2}).Plan(context.Background())
	if err != nil || !reflect.DeepEqual(steps(p), []string{"add runner claude-2", "add runner claude-3"}) {
		t.Fatalf("plan = %v, %v", steps(p), err)
	}
	var pulls, checks int
	for _, c := range f.calls {
		switch {
		case c[0] == "pull":
			pulls++
			if !reflect.DeepEqual(c, []string{"pull", LoginImage}) {
				t.Errorf("pull = %q", c)
			}
		case c[0] == "run":
			checks++
			if pulls != 2 || !hasArg(c, "--pull=never") || c[len(c)-1] != "status" {
				t.Errorf("check %q after %d pulls", c, pulls)
			}
		}
		for _, a := range c {
			if strings.HasPrefix(a, "--policy") {
				t.Errorf("podman %q has --policy", c)
			}
		}
	}
	if pulls != 2 || checks != 1 || log.String() != eofRetry {
		t.Errorf("%d pulls, %d checks, log %q", pulls, checks, log.String())
	}
}

func TestPullCancelledDuringTheWait(t *testing.T) {
	_, _ = stubPullRetry(t)
	ctx, cancel := context.WithCancel(context.Background())
	ghretry.Sleep = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	f := &fakeHost{pullExits: []int{125, 125, 125, 125}, pullErr: eofErr}
	err := PullImage(ctx, f.Podman, LoginImage, "for the claude login check", PullNewer)
	if Code(err) != CodeCancelled || f.pulls != 1 || len(f.calls) != 1 {
		t.Errorf("err = %v, calls %q", err, f.calls)
	}
	if err := PullImage(ctx, f.Podman, LoginImage, "x", PullMissing); Code(err) != CodeCancelled {
		t.Errorf("cancelled before: %v", err)
	}
}

func TestPullCause(t *testing.T) {
	for _, tc := range []struct {
		stderr string
		want   string
	}{
		{"a\n\x1b[31mError: boom\x1b[0m\n\n", "Error: boom"},
		{"", "exit status 3"},
		{strings.Repeat("x", 300), strings.Repeat("x", pullCauseMax) + "…"},
	} {
		if got := pullCause(tc.stderr, errors.New("exit status 3")); got != tc.want {
			t.Errorf("pullCause(%q) = %q", tc.stderr, got)
		}
	}
}

func TestPullErrorKeepsTheStderrTail(t *testing.T) {
	stubPullRetry(t)
	f := &fakeHost{pullExits: []int{1, 1, 1, 1}, pullErr: strings.Repeat("x", 3*pullStderrTail) + "the end", imageExit: 1}
	err := PullImage(context.Background(), f.Podman, LoginImage, "for x", PullMissing)
	if Code(err) != CodePodmanFailed || !strings.HasSuffix(err.Error(), "the end") || len(err.Error()) > pullStderrTail+200 {
		t.Errorf("err = %.80s… (%d bytes)", err, len(err.Error()))
	}
	f = &fakeHost{pullExits: []int{4, 4, 4, 4}, imageExit: 1}
	if err := PullImage(context.Background(), f.Podman, LoginImage, "for x", PullMissing); !strings.HasSuffix(err.Error(), "after 4 tries: exit status 4") {
		t.Errorf("silent failure: %v", err)
	}
}
