package op

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/taco3064/gh-nibfin/internal/runners"
)

// The job-started hook of a collaborator's Runners (request route): one
// directory per host, runners.HookDir, outside every slot. Its constant
// files (internal/runnerhook) reach op through SetRunnerHookFiles; op adds
// the repository's config and, for a private repository, GitHub's SSH host
// keys. Each slot's .env names the hook (ACTIONS_RUNNER_HOOK_JOB_STARTED)
// and the slot (NIBFIN_RUNNER_DIR), so the runner runs it before every Job.

const (
	actInstallHook = "install job-started hook"
	actRemoveHook  = "remove job-started hook"

	// hookHost is the only repository host the hook reads: the attestation
	// source and raw.githubusercontent.com are github.com's.
	hookHost = "github.com"
)

var runnerHookFiles func(context.Context) (map[string][]byte, error)

// SetRunnerHookFiles sets where runner add gets the hook's constant files:
// the scripts, the official workflow files and the trusted root, keyed by
// their path in the hook directory. internal/cmd sets it once at start-up.
func SetRunnerHookFiles(f func(context.Context) (map[string][]byte, error)) { runnerHookFiles = f }

// The hook host scripts. They are constants; only paths travel as their
// arguments and the files as a tar on stdin.
const (
	// hookReadScript prints the mode of the hook directory $1 and each file
	// in it, path and content base64-encoded; nothing when it is absent.
	hookReadScript = `[ -d "$1" ] || exit 0
cd "$1" || exit 1
printf 'mode %s\n' "$(stat -c %a .)"
find . -type f | while IFS= read -r f; do
  printf 'file %s %s\n' "$(printf '%s' "${f#./}" | base64 | tr -d '\n')" "$(base64 < "$f" | tr -d '\n')" || exit 1
done
`
	// hookWriteScript replaces the hook directory $1 with the tar on stdin:
	// unpacked into $1.new (mode 0700, files 0600), then swapped in. It
	// refuses anything but a .../nibfin/hook directory.
	hookWriteScript = `set -e
case $1 in /*/nibfin/hook) ;; *) echo "refusing to write the hook to $1" >&2; exit 2 ;; esac
case $1 in */../*|*/./*) echo "refusing to write the hook to $1" >&2; exit 2 ;; esac
umask 077
mkdir -p "${1%/*}"
rm -rf "$1.new"
mkdir "$1.new"
tar -xf - -C "$1.new"
chmod -R go-rwx "$1.new"
rm -rf "$1.old"
if [ -d "$1" ]; then mv "$1" "$1.old"; fi
mv "$1.new" "$1"
rm -rf "$1.old"
`
	// hookEnvScript sets ACTIONS_RUNNER_HOOK_JOB_STARTED=$2 and
	// NIBFIN_RUNNER_DIR=$1 in the .env of the slot $1: every other line is
	// kept, earlier lines of these keys are dropped, and the file is
	// replaced atomically.
	hookEnvScript = `set -e
f="$1/.env"
if [ -f "$f" ]; then grep -v -e '^ACTIONS_RUNNER_HOOK_JOB_STARTED=' -e '^NIBFIN_RUNNER_DIR=' "$f" > "$f.tmp" || [ $? -eq 1 ]; else : > "$f.tmp"; fi
printf 'ACTIONS_RUNNER_HOOK_JOB_STARTED=%s\nNIBFIN_RUNNER_DIR=%s\n' "$2" "$1" >> "$f.tmp"
mv -f "$f.tmp" "$f"
`
	// hookEnvCheckScript prints yes when the .env of the slot $1 has both
	// hook lines for the hook script $2, otherwise no.
	hookEnvCheckScript = `if grep -qxF "ACTIONS_RUNNER_HOOK_JOB_STARTED=$2" "$1/.env" 2>/dev/null && grep -qxF "NIBFIN_RUNNER_DIR=$1" "$1/.env" 2>/dev/null; then echo yes; else echo no; fi
`
	// hookPresentScript prints present when the hook directory $1 exists.
	hookPresentScript = `if [ -d "$1" ]; then echo present; fi
`
	// hookRemoveScript deletes the hook directory $1 and what an
	// interrupted write left beside it. It refuses anything but a
	// .../nibfin/hook directory.
	hookRemoveScript = `case $1 in /*/nibfin/hook) ;; *) echo "refusing to remove $1" >&2; exit 2 ;; esac
case $1 in */../*|*/./*) echo "refusing to remove $1" >&2; exit 2 ;; esac
rm -rf -- "$1" "$1.new" "$1.old"
`
)

// hookPathRE is what a path the hook names may hold: it is quoted in a
// shell command line (GIT_SSH_COMMAND) and written to a line-based config.
var hookPathRE = regexp.MustCompile(`^/[A-Za-z0-9._/+@ -]+$`)

// hookRepo splits HOST/OWNER/REPO for the hook; another host than
// github.com is unsupported_host.
func hookRepo(repo string) (owner, name string, err error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 3 || !strings.EqualFold(parts[0], hookHost) {
		return "", "", Errorf(CodeUnsupportedHost, "%s is not on %s: a collaborator's Runner checks its Jobs against github.com only", repo, hookHost)
	}
	re := regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	if !re.MatchString(parts[1]) || !re.MatchString(parts[2]) {
		return "", "", Errorf(CodeInvalidArgument, "%q is not a repository", repo)
	}
	return parts[1], parts[2], nil
}

// hookSpec is what the hook files of a host depend on.
type hookSpec struct {
	inv     *runners.Inventory
	repo    string
	userID  int64
	private bool
	gh      github
}

// hookFiles are the files the hook directory must hold: the constant files,
// the config and, for a private repository, GitHub's SSH host keys. A
// failure is runner_install_failed with its cause.
func hookFiles(ctx context.Context, s hookSpec) (map[string][]byte, error) {
	owner, name, err := hookRepo(s.repo)
	if err != nil {
		return nil, err
	}
	dir := runners.HookDir(s.inv)
	if !hookPathRE.MatchString(dir) {
		return nil, Errorf(CodeRunnerInstallFailed, "the hook directory %q has characters the hook cannot use", dir)
	}
	if runnerHookFiles == nil {
		return nil, Errorf(CodeRunnerInstallFailed, "the job-started hook files are not available in this build")
	}
	files, err := runnerHookFiles(ctx)
	if err != nil {
		return nil, Errorf(CodeRunnerInstallFailed, "the job-started hook files: %v", err)
	}
	out := make(map[string][]byte, len(files)+2)
	for k, v := range files {
		out[k] = v
	}
	cfg := fmt.Sprintf("host=%s\nowner=%s\nname=%s\nuser-id=%d\n", hookHost, owner, name, s.userID)
	if s.private {
		key := runners.DeployKeyPath(s.inv, s.repo)
		if !hookPathRE.MatchString(key) {
			return nil, Errorf(CodeRunnerInstallFailed, "the deploy key path %q has characters the hook cannot use", key)
		}
		cfg += "private=1\ndeploy-key=" + key + "\n"
		keys, err := s.gh.SSHHostKeys(ctx)
		if err != nil {
			return nil, err
		}
		var kh strings.Builder
		for _, k := range keys {
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(k))
			if err != nil {
				return nil, Errorf(CodeGitHubAPIFailed, "GitHub's SSH host key %q is not a public key", k)
			}
			kh.WriteString(hookHost + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + "\n")
		}
		if kh.Len() == 0 {
			return nil, Errorf(CodeGitHubAPIFailed, "GitHub lists no SSH host keys")
		}
		out["known_hosts"] = []byte(kh.String())
	} else {
		cfg += "private=0\n"
	}
	out["config"] = []byte(cfg)
	return out, nil
}

// hookTar packs files into a tar, directories 0700 and files 0600.
func hookTar(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	dirs := map[string]bool{}
	mtime := time.Unix(0, 0)
	for _, n := range names {
		if path.IsAbs(n) || path.Clean(n) != n || n == ".." || strings.HasPrefix(n, "../") {
			return nil, fmt.Errorf("bad hook file name %q", n)
		}
		if d := path.Dir(n); d != "." && !dirs[d] {
			dirs[d] = true
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: d + "/", Mode: 0o700, ModTime: mtime}); err != nil {
				return nil, err
			}
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: n, Mode: 0o600, Size: int64(len(files[n])), ModTime: mtime}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// hookState is the hook directory as read from the host.
type hookState struct {
	present bool
	mode    string
	files   map[string][]byte
}

// readHook reads the hook directory dir from the host.
func readHook(ctx context.Context, h runnerHost, code, dir string) (hookState, error) {
	out, _, err := hostScript(ctx, h, code, "read the job-started hook", hookReadScript, nil, "", dir)
	if err != nil {
		return hookState{}, err
	}
	st := hookState{files: map[string][]byte{}}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 64<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), " ")
		switch {
		case f[0] == "mode" && len(f) == 2:
			st.present, st.mode = true, f[1]
		case f[0] == "file" && len(f) == 3:
			p, err1 := base64.StdEncoding.DecodeString(f[1])
			b, err2 := base64.StdEncoding.DecodeString(f[2])
			if err1 != nil || err2 != nil {
				return st, Errorf(code, "read the job-started hook: bad line")
			}
			st.files[string(p)] = b
		default:
			return st, Errorf(code, "read the job-started hook: unexpected line %q", sc.Text())
		}
	}
	return st, sc.Err()
}

// diff names how st differs from want; "" when it holds exactly want with
// the directory at mode 0700.
func (st hookState) diff(want map[string][]byte) string {
	if !st.present {
		return "absent"
	}
	var why []string
	for _, n := range sortedKeys(want) {
		got, ok := st.files[n]
		switch {
		case !ok:
			why = append(why, n+" missing")
		case !bytes.Equal(got, want[n]):
			why = append(why, n+" differs")
		}
	}
	for _, n := range sortedKeys(st.files) {
		if _, ok := want[n]; !ok {
			why = append(why, n+" unexpected")
		}
	}
	if st.mode != "700" {
		why = append(why, "directory mode 0"+st.mode)
	}
	if len(why) == 0 {
		return ""
	}
	return "outdated: " + strings.Join(why, ", ")
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// hookStep is runner add's hook item for the host's hook script; current
// is "" when the files are in place.
func hookStep(inv *runners.Inventory, login string, private bool, current string) Step {
	how := "public, read without a credential"
	if private {
		how = "private, read with the deploy key"
	}
	s := Step{ID: "hook", Action: actInstallHook, Target: runners.HookScript(inv),
		Detail: "only official Nibfin Jobs for " + login + "; " + how + "; image signature checked against Sigstore Public Good",
		Next:   "current"}
	if current == "" {
		s.Current, s.NoChange = "current", true
	} else {
		s.Current = current
	}
	return s
}

// planHook reads the host's hook and returns its item; it keeps the files
// expected for Apply and the read-back.
func (a *runnerAdd) planHook(ctx context.Context, env *addEnv) (Step, error) {
	want, err := hookFiles(ctx, env.hookSpec())
	if err != nil {
		return Step{}, err
	}
	a.hookWant = want
	st, err := readHook(ctx, a.host, CodeRunnerInstallFailed, runners.HookDir(env.inv))
	if err != nil {
		return Step{}, err
	}
	return hookStep(env.inv, env.user.Login, env.private, st.diff(want)), nil
}

func (env *addEnv) hookSpec() hookSpec {
	return hookSpec{inv: env.inv, repo: env.repo, userID: env.user.ID, private: env.private, gh: env.gh}
}

// writeHook is the hook item: it writes the expected files to the host's
// hook directory, replacing the directory whole.
func (a *runnerAdd) writeHook(ctx context.Context, env *addEnv) error {
	want, err := hookFiles(ctx, env.hookSpec())
	if err != nil {
		return err
	}
	a.hookWant = want
	b, err := hookTar(want)
	if err != nil {
		return Errorf(CodeRunnerInstallFailed, "pack the job-started hook: %v", err)
	}
	_, err = a.script(ctx, "write the job-started hook", hookWriteScript, bytes.NewReader(b), "", runners.HookDir(env.inv))
	return err
}

// hookInEnv reads back whether the slot's .env names the hook.
func hookInEnv(ctx context.Context, h runnerHost, code string, inv *runners.Inventory, slot string) (bool, error) {
	out, _, err := hostScript(ctx, h, code, "read the .env of "+slot, hookEnvCheckScript, nil, "", inv.SlotDir(slot), runners.HookScript(inv))
	return strings.TrimSpace(out) == "yes", err
}

// unhookedNote names the complete slots that run without the hook: added
// on the request route before runner add installed it.
func (a *runnerAdd) unhookedNote(ctx context.Context, env *addEnv, chosen []string) (string, error) {
	skip := map[string]bool{}
	for _, s := range chosen {
		skip[s] = true
	}
	var without []string
	for _, r := range env.inv.Runners {
		if skip[r.Slot] || !r.Complete() {
			continue
		}
		ok, err := hookInEnv(ctx, a.host, CodeRunnerInstallFailed, env.inv, r.Slot)
		if err != nil {
			return "", err
		}
		if !ok {
			without = append(without, r.Slot)
		}
	}
	if len(without) == 0 {
		return "", nil
	}
	return fmt.Sprintf("The Runners %s were added before the job-started hook and run every Job routed to them; runner add leaves them as they are. "+
		"To give them the hook, run gh nibfin runner remove for their provider, then gh nibfin runner add.", strings.Join(without, ", ")), nil
}

// readBackHook reads the hook item back: the host's hook directory holds
// the expected files. The expected files are those Plan or Apply built,
// else built again from GitHub.
func (a *runnerAdd) readBackHook(ctx context.Context, gh github, inv *runners.Inventory, repo string, checks *Checks, r Result) error {
	step := Step{ID: "hook", Action: actInstallHook, Target: runners.HookScript(inv)}
	checks.Expect(step)
	want := a.hookWant
	if want == nil {
		user, err := gh.User(ctx)
		if err != nil {
			return err
		}
		perms, err := gh.RepoPermissions(ctx, repo)
		if err != nil {
			return err
		}
		if want, err = hookFiles(ctx, hookSpec{inv: inv, repo: repo, userID: user.ID, private: perms.Private, gh: gh}); err != nil {
			return err
		}
	}
	st, err := readHook(ctx, a.host, CodeRunnerInstallFailed, runners.HookDir(inv))
	if err != nil {
		return err
	}
	why := st.diff(want)
	state := "current, mode 0700"
	if why != "" {
		state = why
	}
	checks.Set(step, why == "", state, "the job-started hook is "+why)
	r["hook"] = step.Target
	return nil
}

// hookPresent reports whether the hook directory dir exists on the host.
func hookPresent(ctx context.Context, h runnerHost, code, dir string) (bool, error) {
	out, _, err := hostScript(ctx, h, code, "look for the job-started hook", hookPresentScript, nil, "", dir)
	return strings.TrimSpace(out) == "present", err
}

// removeHookStep is runner remove's hook item.
func removeHookStep(dir, where string) Step {
	return Step{ID: "hook", Action: actRemoveHook, Target: dir, Detail: "no runner remains on " + where, Current: "present", Next: "absent"}
}

// toolFlag parses a probe tools field.
func toolFlag(s string) bool {
	v, _ := strconv.Atoi(s)
	return v == 1
}
