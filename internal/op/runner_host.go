package op

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// icuCheck defines icu, which succeeds when libicu is found the way the
// official runner's config.sh looks for it on Linux: ldconfig (or
// /sbin/ldconfig) over the LD_LIBRARY_PATH entries and the system paths.
const icuCheck = `icu() {
  l=ldconfig
  command -v ldconfig >/dev/null 2>&1 || l=/sbin/ldconfig
  "$l" -NXv $(printf '%s' "${LD_LIBRARY_PATH:-}" | tr ':' ' ') 2>&1 | grep -q libicu
}
`

// probeScript prints what the runner commands and status need to know
// about the host.
const probeScript = icuCheck + `u=$(id -u)
n=$(id -un)
printf 'arch %s\n' "$(uname -m | tr -cd 'A-Za-z0-9_')"
printf 'node %s\n' "$(uname -n | tr -cd 'A-Za-z0-9.-')"
printf 'machine %s\n' "$(tr -cd '0-9a-fA-F' < /etc/machine-id 2>/dev/null)"
if [ -e "/var/lib/systemd/linger/$n" ]; then echo 'linger 1'; else echo 'linger 0'; fi
c="/sys/fs/cgroup/user.slice/user-$u.slice/user@$u.service/cgroup.controllers"
if [ -r "$c" ]; then printf 'cgroup %s\n' "$(tr -cd 'a-z_ ' < "$c")"; else echo 'cgroup ?'; fi
printf 'memkb %s\n' "$(awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null | tr -cd '0-9')"
printf 'memavailkb %s\n' "$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null | tr -cd '0-9')"
t() { if command -v "$1" >/dev/null 2>&1; then echo 1; else echo 0; fi; }
printf 'tools %s %s\n' "$(t git)" "$(t ssh)"
if icu; then echo 'icu 1'; else echo 'icu 0'; fi
`

type hostProbe struct {
	arch, node, machine string
	linger              bool
	cgroupRead          bool
	controllers         map[string]bool
	memKB               int64
	memAvailKB          int64
	// git and ssh are installed: the hook reads a private repository with
	// them.
	git, ssh bool
	// icu is true when the official runner's config.sh finds libicu.
	icu bool
}

// hostScript runs a fixed runner-host script. Its stdout and exit status are
// returned; a failure is code with the script's stderr, or podman_failed
// when podman machine ssh itself failed. secret is scrubbed from the error.
func hostScript(ctx context.Context, h runnerHost, code, what, script string, stdin io.Reader, secret string, args ...string) (string, int, error) {
	return runHostScript(ctx, h, code, what, script, stdin, secret, false, args...)
}

// hostScriptOutput is hostScript whose failure keeps the child's exit status
// and the masked, bounded end of both its stderr and its stdout.
func hostScriptOutput(ctx context.Context, h runnerHost, code, what, script string, stdin io.Reader, secret string, args ...string) (string, int, error) {
	return runHostScript(ctx, h, code, what, script, stdin, secret, true, args...)
}

func runHostScript(ctx context.Context, h runnerHost, code, what, script string, stdin io.Reader, secret string, keepOutput bool, args ...string) (string, int, error) {
	var stdout, stderr bytes.Buffer
	c := h.Shell(ctx, script, args...)
	c.Stdin = stdin
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if err == nil {
		return stdout.String(), 0, nil
	}
	if ctx.Err() != nil {
		return "", -1, Errorf(CodeCancelled, "cancelled")
	}
	exit := exitCode(err)
	var msg string
	if keepOutput {
		what = maskSecret(what, secret)
		msg = childFailure(err, exit, stderr.String(), stdout.String(), secret)
	} else {
		msg = maskSecret(stderrText(err, &stderr), secret)
	}
	if !h.Local() && exit == 125 {
		return "", exit, Errorf(CodePodmanFailed, "podman machine ssh (%s): %s", what, msg)
	}
	return "", exit, Errorf(code, "%s on %s: %s", what, h.Describe(), msg)
}

// The kept end of each child output stream.
const (
	childTailLines = 20
	childTailBytes = 2048
)

const childCutMark = "[earlier output cut] "

// maskSecret replaces every occurrence of secret in s.
func maskSecret(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[token]")
}

// childFailure describes a failed child: its exit status, then each
// non-empty output stream, labelled, masked and cut to its last lines.
func childFailure(err error, exit int, stderr, stdout, secret string) string {
	msg := err.Error()
	if exit >= 0 {
		msg = "exit status " + strconv.Itoa(exit)
	}
	for _, s := range []struct{ name, text string }{{"stderr", stderr}, {"stdout", stdout}} {
		if t := outputTail(maskSecret(s.text, secret)); t != "" {
			msg += "; " + s.name + ": " + t
		}
	}
	return msg
}

// outputTail keeps the last non-empty lines of out, at most childTailLines
// lines and childTailBytes bytes, joined by " | ". Earlier output that was
// dropped is marked.
func outputTail(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	cut := len(lines) > childTailLines
	if cut {
		lines = lines[len(lines)-childTailLines:]
	}
	// Keep whole lines from the end while they fit; a last line longer than
	// the cap keeps its own end, on a rune boundary.
	start, size := len(lines), 0
	for start > 0 {
		n := len(lines[start-1])
		if start < len(lines) {
			n += len(" | ")
		}
		if size+n > childTailBytes {
			break
		}
		size += n
		start--
	}
	if start == len(lines) {
		l := lines[len(lines)-1]
		l = l[len(l)-childTailBytes:]
		for len(l) > 0 && !utf8.RuneStart(l[0]) {
			l = l[1:]
		}
		lines[len(lines)-1] = l
		start, cut = len(lines)-1, true
	}
	if start > 0 {
		cut = true
	}
	t := strings.Join(lines[start:], " | ")
	if cut {
		t = childCutMark + t
	}
	return t
}

// probeHost runs probeScript; a failure is code.
func probeHost(ctx context.Context, h runnerHost, code string) (hostProbe, error) {
	Checking(ctx, "host settings")
	out, _, err := hostScript(ctx, h, code, "probe the runner host", probeScript, nil, "")
	if err != nil {
		return hostProbe{}, err
	}
	pr := hostProbe{controllers: map[string]bool{}}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		key, val, _ := strings.Cut(sc.Text(), " ")
		switch key {
		case "arch":
			pr.arch = val
		case "node":
			pr.node = val
		case "machine":
			pr.machine = strings.ToLower(val)
		case "linger":
			pr.linger = val == "1"
		case "cgroup":
			if val != "?" {
				pr.cgroupRead = true
				for _, c := range strings.Fields(val) {
					pr.controllers[c] = true
				}
			}
		case "memkb":
			pr.memKB, _ = strconv.ParseInt(val, 10, 64)
		case "memavailkb":
			pr.memAvailKB, _ = strconv.ParseInt(val, 10, 64)
		case "icu":
			pr.icu = val == "1"
		case "tools":
			f := strings.Fields(val)
			if len(f) == 2 {
				pr.git, pr.ssh = toolFlag(f[0]), toolFlag(f[1])
			}
		default:
			return pr, Errorf(code, "probe the runner host: unexpected line %q", sc.Text())
		}
	}
	return pr, nil
}

var notNameChar = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// runnerName is <hostname>-<hostid>-<provider>-<n>: unique to this host, so
// --replace only ever replaces this slot's own earlier registration.
func (pr hostProbe) runnerName(slot string) (string, error) {
	if pr.machine == "" {
		return "", Errorf(CodeRunnerInstallFailed, "the runner host has no /etc/machine-id to name its runners by")
	}
	node := strings.Trim(notNameChar.ReplaceAllString(pr.node, "-"), "-")
	if len(node) > 32 {
		node = strings.Trim(node[:32], "-")
	}
	if node == "" {
		node = "host"
	}
	sum := sha256.Sum256([]byte("gh-nibfin runner\n" + pr.machine))
	return node + "-" + hex.EncodeToString(sum[:])[:8] + "-" + slot, nil
}
