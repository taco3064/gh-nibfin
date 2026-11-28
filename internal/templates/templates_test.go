package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/ghretry"
	"github.com/taco3064/gh-nibfin/internal/op"
)

const (
	testImage  = "ghcr.io/taco3064/nibfin@sha256:" + "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	testCommit = "4ed4c16bff8600000000000000000000000000ab"
)

// fakePodman records the podman arguments and answers with this test binary
// run as TestRenderHelper, which prints stdout and stderr and exits with
// exit. The files it prints are made up, not Core's. args is the last run;
// calls are every command. podman image exists exits with existsExit, and
// the n-th podman pull with pullExits[n] (0 past its end) and pullStderr.
type fakePodman struct {
	args           []string
	calls          [][]string
	stdout, stderr string
	exit           int
	existsExit     int
	pullExits      []int
	pullStderr     string
	pulls          int
}

func (f *fakePodman) run(ctx context.Context, args ...string) *exec.Cmd {
	f.calls = append(f.calls, args)
	stdout, stderr, exit := f.stdout, f.stderr, f.exit
	switch args[0] {
	case "image":
		stdout, stderr, exit = "", "", f.existsExit
	case "pull":
		stdout, stderr, exit = "", f.pullStderr, 0
		if f.pulls < len(f.pullExits) {
			exit = f.pullExits[f.pulls]
		}
		f.pulls++
	default:
		f.args = args
	}
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRenderHelper$")
	c.Env = append(os.Environ(), "NIBFIN_RENDER_HELPER=1", "HELPER_STDOUT="+stdout, "HELPER_STDERR="+stderr, "HELPER_EXIT="+strconv.Itoa(exit))
	return c
}

// TestRenderHelper is the fake podman of fakePodman.
func TestRenderHelper(t *testing.T) {
	if os.Getenv("NIBFIN_RENDER_HELPER") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, os.Getenv("HELPER_STDOUT"))
	fmt.Fprint(os.Stderr, os.Getenv("HELPER_STDERR"))
	code, _ := strconv.Atoi(os.Getenv("HELPER_EXIT"))
	os.Exit(code)
}

// output is render-workflows' stdout holding a made-up file for each path.
func output(t *testing.T, paths ...string) string {
	t.Helper()
	m := map[string]string{}
	for _, p := range paths {
		m[p] = GeneratedPrefix + "\nname: " + p + "\n"
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func params() Params {
	return Params{Image: testImage, Commit: testCommit, DefaultBranch: "main", Workflows: []string{"CI", "-x", "Deploy"}}
}

func TestRenderRunsCore(t *testing.T) {
	for _, tc := range []struct {
		set  Set
		name string
	}{{Execution, "execution"}, {Peer, "peer"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakePodman{stdout: output(t, Files(tc.set)...)}
			files, err := Render(context.Background(), f.run, tc.set, params())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			want := []string{"run", "--rm", "--pull=never", "--network=none", testImage, "nibfin", "render-workflows",
				"--set=" + tc.name, "--channel=user", "--image=" + testImage, "--commit=" + testCommit,
				"--default-branch=main", "--workflow=CI", "--workflow=-x", "--workflow=Deploy"}
			if !slices.Equal(f.args, want) {
				t.Errorf("podman args\n %q\nwant\n %q", f.args, want)
			}
			if len(files) != len(Files(tc.set)) {
				t.Errorf("%d files, want %d", len(files), len(Files(tc.set)))
			}
			for _, p := range Files(tc.set) {
				if string(files[p]) != GeneratedPrefix+"\nname: "+p+"\n" {
					t.Errorf("%s = %q", p, files[p])
				}
			}
		})
	}
}

func TestRenderRunsInAnotherImage(t *testing.T) {
	const run = "ghcr.io/taco3064/nibfin@sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	f := &fakePodman{stdout: output(t, Files(Execution)...)}
	p := params()
	p.Run, p.Workflows = run, nil
	if _, err := Render(context.Background(), f.run, Execution, p); err != nil {
		t.Fatal(err)
	}
	if f.args[4] != run || !slices.Contains(f.args, "--image="+testImage) || slices.ContainsFunc(f.args, func(a string) bool { return strings.HasPrefix(a, "--workflow") }) {
		t.Errorf("podman args %q", f.args)
	}
}

func TestRenderFailures(t *testing.T) {
	exec := Files(Execution)
	for name, tc := range map[string]struct {
		podman   fakePodman
		set      Set
		code     string
		contains string
	}{
		"Core rejects an input": {podman: fakePodman{stderr: "nibfin: WORKFLOW_INPUT_INVALID: default branch \"a\\\"b\" has a quote\n", exit: 1},
			code: op.CodeInvalidTemplateInput, contains: `rejected the input: default branch "a\"b" has a quote`},
		"another Core error": {podman: fakePodman{stderr: "nibfin: INTERNAL: boom\n", exit: 1}, code: op.CodePodmanFailed, contains: "nibfin: INTERNAL: boom"},
		"a pull failure": {podman: fakePodman{stderr: "Error: initializing source docker://ghcr.io/x: unauthorized\n", exit: 125},
			code: op.CodePodmanFailed, contains: "unauthorized"},
		"a silent failure":     {podman: fakePodman{exit: 2}, code: op.CodePodmanFailed, contains: "exit status 2"},
		"not JSON":             {podman: fakePodman{stdout: "hello\n"}, code: op.CodeInvalidTemplateInput, contains: "no JSON object"},
		"JSON of another type": {podman: fakePodman{stdout: `["a"]`}, code: op.CodeInvalidTemplateInput, contains: "no JSON object"},
		"a missing file": {podman: fakePodman{stdout: output(t, exec[:3]...)}, code: op.CodeInvalidTemplateInput,
			contains: "did not print the execution set: it lacks " + exec[3]},
		"an extra file": {podman: fakePodman{stdout: output(t, append(Files(Peer), ".github/workflows/evil.yml")...)}, set: Peer,
			code: op.CodeInvalidTemplateInput, contains: "it adds .github/workflows/evil.yml"},
		"a file without the header": {podman: fakePodman{stdout: `{".github/workflows/nibfin-issue.yml":"name: x\n"}`}, set: Peer,
			code: op.CodeInvalidTemplateInput, contains: `nibfin-issue.yml without the "# Generated by gh nibfin" header`},
		"an unknown set": {podman: fakePodman{stdout: "{}"}, set: Set(9), code: op.CodeInvalidTemplateInput, contains: "unknown template set"},
	} {
		t.Run(name, func(t *testing.T) {
			f := tc.podman
			files, err := Render(context.Background(), f.run, tc.set, params())
			if files != nil {
				t.Errorf("returned files despite the failure")
			}
			if op.Code(err) != tc.code || !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("err = %v, want %s with %q", err, tc.code, tc.contains)
			}
		})
	}
}

func TestRenderKeepsTheStderrTail(t *testing.T) {
	f := &fakePodman{stderr: strings.Repeat("x", 3*stderrTail) + "the end", exit: 125}
	_, err := Render(context.Background(), f.run, Peer, params())
	if op.Code(err) != op.CodePodmanFailed || !strings.HasSuffix(err.Error(), "the end") || len(err.Error()) > stderrTail+200 {
		t.Errorf("err = %.80s… (%d bytes)", err, len(err.Error()))
	}
}

func TestRenderCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakePodman{stdout: output(t, Files(Peer)...)}
	if _, err := Render(ctx, f.run, Peer, params()); op.Code(err) != op.CodeCancelled {
		t.Errorf("err = %v", err)
	}
}

func TestFiles(t *testing.T) {
	if got := Files(Peer); !slices.Equal(got, []string{".github/workflows/nibfin-issue.yml"}) {
		t.Errorf("Files(Peer) = %v", got)
	}
	got := Files(Execution)
	if len(got) != 4 || got[0] != ".github/workflows/nibfin-issue.yml" || got[3] != ".github/workflows/"+RunnerRequestFile {
		t.Errorf("Files(Execution) = %v", got)
	}
	if Files(Set(9)) != nil {
		t.Error("Files of an unknown set")
	}
}

// stubRetry makes pull retries record their waits instead of sleeping and
// collects their lines.
func stubRetry(t *testing.T) (*strings.Builder, *[]time.Duration) {
	t.Helper()
	oldSleep, oldLog := ghretry.Sleep, ghretry.Log
	var log strings.Builder
	var waits []time.Duration
	ghretry.Sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	ghretry.Log = &log
	t.Cleanup(func() { ghretry.Sleep, ghretry.Log = oldSleep, oldLog })
	return &log, &waits
}

func verbs(calls [][]string) []string {
	out := []string{}
	for _, c := range calls {
		out = append(out, strings.Join(c[:2], " "))
	}
	return out
}

func TestRenderPullsOnlyAMissingImage(t *testing.T) {
	log, _ := stubRetry(t)
	f := &fakePodman{stdout: output(t, Files(Peer)...)}
	if _, err := Render(context.Background(), f.run, Peer, params()); err != nil {
		t.Fatal(err)
	}
	if got := verbs(f.calls); !slices.Equal(got, []string{"image exists", "run --rm"}) || !slices.Equal(f.calls[0], []string{"image", "exists", testImage}) {
		t.Errorf("present image: calls %q", f.calls)
	}

	f = &fakePodman{stdout: output(t, Files(Peer)...), existsExit: 1, pullExits: []int{125}, pullStderr: "Copying blob\nError: failed to decode message from stream: unexpected EOF\n"}
	if _, err := Render(context.Background(), f.run, Peer, params()); err != nil {
		t.Fatal(err)
	}
	if got := verbs(f.calls); !slices.Equal(got, []string{"image exists", "pull " + testImage, "pull " + testImage, "run --rm"}) {
		t.Errorf("missing image: calls %q", got)
	}
	want := "nibfin: podman pull " + testImage + " failed (Error: failed to decode message from stream: unexpected EOF); retry 1 of 3\n"
	if log.String() != want {
		t.Errorf("log = %q\nwant %q", log.String(), want)
	}
}

func TestRenderFailsAfterFourPulls(t *testing.T) {
	_, waits := stubRetry(t)
	f := &fakePodman{existsExit: 1, pullExits: []int{125, 125, 125, 125}, pullStderr: "Error: unexpected EOF\n"}
	_, err := Render(context.Background(), f.run, Peer, params())
	if op.Code(err) != op.CodePodmanFailed || !strings.Contains(err.Error(), "after 4 tries: Error: unexpected EOF") || !strings.Contains(err.Error(), testImage) {
		t.Errorf("err = %v", err)
	}
	if f.pulls != 4 || f.args != nil {
		t.Errorf("%d pulls, run %q", f.pulls, f.args)
	}
	if !slices.Equal(*waits, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
		t.Errorf("waits = %v", *waits)
	}
}

func TestRenderRunFailureIsNotRetried(t *testing.T) {
	log, _ := stubRetry(t)
	for _, exit := range []int{1, 125} {
		f := &fakePodman{stderr: "Error: boom\n", exit: exit, existsExit: 1}
		_, err := Render(context.Background(), f.run, Peer, params())
		if op.Code(err) != op.CodePodmanFailed || !slices.Equal(verbs(f.calls), []string{"image exists", "pull " + testImage, "run --rm"}) {
			t.Errorf("exit %d: err %v, calls %q", exit, err, f.calls)
		}
	}
	if log.Len() != 0 {
		t.Errorf("log = %q", log.String())
	}
}
