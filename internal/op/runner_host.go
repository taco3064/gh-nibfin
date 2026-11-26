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
)

// probeScript prints what the runner commands and status need to know
// about the host.
const probeScript = `u=$(id -u)
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
}

// hostScript runs a fixed runner-host script. Its stdout and exit status are
// returned; a failure is code with the script's stderr, or podman_failed
// when podman machine ssh itself failed. secret is scrubbed from the error.
func hostScript(ctx context.Context, h runnerHost, code, what, script string, stdin io.Reader, secret string, args ...string) (string, int, error) {
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
	msg := stderrText(err, &stderr)
	if secret != "" {
		msg = strings.ReplaceAll(msg, secret, "[token]")
	}
	if !h.Local() && exit == 125 {
		return "", exit, Errorf(CodePodmanFailed, "podman machine ssh (%s): %s", what, msg)
	}
	return "", exit, Errorf(code, "%s on %s: %s", what, h.Describe(), msg)
}

// probeHost runs probeScript; a failure is code.
func probeHost(ctx context.Context, h runnerHost, code string) (hostProbe, error) {
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
