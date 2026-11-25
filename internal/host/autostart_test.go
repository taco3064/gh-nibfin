package host

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

// TestToolHelper is a fake host tool: it saves its stdin to
// HELPER_STDIN_FILE when set, prints HELPER_STDOUT (base64) and
// HELPER_STDERR, and exits with HELPER_EXIT.
func TestToolHelper(t *testing.T) {
	if os.Getenv("GO_WANT_TOOL_HELPER") != "1" {
		return
	}
	if f := os.Getenv("HELPER_STDIN_FILE"); f != "" {
		if b, _ := io.ReadAll(os.Stdin); len(b) > 0 {
			os.WriteFile(f, b, 0o644)
		}
	}
	out, _ := base64.StdEncoding.DecodeString(os.Getenv("HELPER_STDOUT"))
	os.Stdout.Write(out)
	fmt.Fprint(os.Stderr, os.Getenv("HELPER_STDERR"))
	exit, _ := strconv.Atoi(os.Getenv("HELPER_EXIT"))
	os.Exit(exit)
}

// reply is what a fake tool prints and its exit status.
type reply struct {
	stdout, stderr string
	exit           int
}

// toolCall is one command, with a PowerShell script decoded to its name.
type toolCall struct {
	name string
	args []string
}

var scriptNames = map[string]string{
	readTaskScript:     "read",
	registerTaskScript: "register",
	runTaskScript:      "run",
	deleteTaskScript:   "delete",
}

// tools installs fakes for the host tools; handle answers each call. The
// stdin of the last call is saved in the returned file.
type tools struct {
	calls []toolCall
	stdin string
}

func fakeTools(t *testing.T, os_ string, handle func(c toolCall) reply) *tools {
	t.Helper()
	oldCmd, oldOS, oldHome, oldUID, oldUser, oldWait := command, goos, homeDir, getuid, currentUser, waitExit
	t.Cleanup(func() {
		command, goos, homeDir, getuid, currentUser, waitExit = oldCmd, oldOS, oldHome, oldUID, oldUser, oldWait
	})
	goos = os_
	home := t.TempDir()
	homeDir = func() (string, error) { return home, nil }
	getuid = func() int { return 501 }
	currentUser = func() (string, string, error) { return `PC\Ann`, "S-1-5-21-1-2-3-1001", nil }
	waitExit = func(context.Context, int) error { return nil }
	tl := &tools{stdin: filepath.Join(t.TempDir(), "stdin")}
	command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		c := toolCall{name, args}
		if name == "powershell.exe" && len(args) == 4 && args[2] == "-EncodedCommand" {
			c.args = []string{scriptNames[decodeScript(t, args[3])]}
		}
		tl.calls = append(tl.calls, c)
		r := handle(c)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestToolHelper$")
		cmd.Env = []string{"GO_WANT_TOOL_HELPER=1", "HELPER_STDIN_FILE=" + tl.stdin,
			"HELPER_STDOUT=" + base64.StdEncoding.EncodeToString([]byte(r.stdout)),
			"HELPER_STDERR=" + r.stderr, "HELPER_EXIT=" + strconv.Itoa(r.exit)}
		return cmd
	}
	return tl
}

func decodeScript(t *testing.T, s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b)%2 != 0 {
		t.Fatalf("bad -EncodedCommand %q", s)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

func (tl *tools) names() []string {
	var out []string
	for _, c := range tl.calls {
		out = append(out, c.name+" "+strings.Join(c.args, " "))
	}
	return out
}

const winPodman = `C:\Program Files\RedHat\Podman\podman.exe`

func winHost() *Host {
	return &Host{podman: winPodman, Machine: "podman-machine-default", VMType: "wsl"}
}

func TestTaskDocument(t *testing.T) {
	h := winHost()
	doc, err := h.taskDocument(`PC\Ann`)
	if err != nil {
		t.Fatal(err)
	}
	s := string(doc)
	for _, want := range []string{
		`<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task" version="1.2">`,
		`<UserId>PC\Ann</UserId>`,
		`<Subscription>&lt;QueryList&gt;&lt;Query Id=&#34;0&#34; Path=&#34;System&#34;&gt;&lt;Select Path=&#34;System&#34;&gt;*[System[Provider[@Name=&#39;Microsoft-Windows-Power-Troubleshooter&#39;] and EventID=1]]&lt;/Select&gt;&lt;/Query&gt;&lt;/QueryList&gt;</Subscription>`,
		`<LogonType>InteractiveToken</LogonType>`,
		`<RunLevel>LeastPrivilege</RunLevel>`,
		`<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>`,
		`<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>`,
		`<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<Command>conhost.exe</Command>`,
		`<Arguments>--headless &#34;C:\Program Files\RedHat\Podman\podman.exe&#34; machine start podman-machine-default</Arguments>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("task XML lacks %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Repetition") {
		t.Error("the task repeats")
	}
	parsed, err := parseTask(doc)
	if err != nil {
		t.Fatal(err)
	}
	if d := h.taskDiff(parsed, `pc\ann`, ""); d != "" {
		t.Errorf("own document differs: %s", d)
	}
}

// exported is a task as the scheduler exports it: UTF-16 declared, the
// principal by SID, defaults omitted.
func exported(mut func(string) string) string {
	x := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><URI>\Nibfin podman machine</URI></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled><UserId>PC\Ann</UserId></LogonTrigger>
    <EventTrigger><Enabled>true</Enabled><Subscription>&lt;QueryList&gt;&lt;Query Id="0" Path="System"&gt;&lt;Select Path="System"&gt;*[System[Provider[@Name='Microsoft-Windows-Power-Troubleshooter'] and EventID=1]]&lt;/Select&gt;&lt;/Query&gt;&lt;/QueryList&gt;</Subscription></EventTrigger>
  </Triggers>
  <Principals><Principal id="Author"><UserId>S-1-5-21-1-2-3-1001</UserId><LogonType>InteractiveToken</LogonType></Principal></Principals>
  <Settings>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <IdleSettings><StopOnIdleEnd>true</StopOnIdleEnd></IdleSettings>
  </Settings>
  <Actions Context="Author"><Exec><Command>conhost.exe</Command><Arguments>--headless "C:\Program Files\RedHat\Podman\podman.exe" machine start podman-machine-default</Arguments></Exec></Actions>
</Task>`
	if mut != nil {
		x = mut(x)
	}
	return x
}

func TestTaskDiff(t *testing.T) {
	h := winHost()
	for _, tc := range []struct {
		name string
		mut  func(string) string
		want string
	}{
		{"exported", nil, ""},
		{"no resume trigger", func(x string) string {
			i, j := strings.Index(x, "<EventTrigger>"), strings.Index(x, "</EventTrigger>")
			return x[:i] + x[j+len("</EventTrigger>"):]
		}, "triggers"},
		{"extra trigger", func(x string) string {
			return strings.Replace(x, "</Triggers>", "<BootTrigger/></Triggers>", 1)
		}, "triggers"},
		{"other podman", func(x string) string { return strings.Replace(x, `RedHat\Podman`, `Old`, 1) }, "another podman"},
		{"battery", func(x string) string {
			return strings.Replace(x, "<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>", "", 1)
		}, "battery"},
		{"time limit", func(x string) string {
			return strings.Replace(x, "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "", 1)
		}, "time limit"},
		{"repeats", func(x string) string {
			return strings.Replace(x, "<UserId>PC\\Ann</UserId></LogonTrigger>", "<UserId>PC\\Ann</UserId><Repetition><Interval>PT5M</Interval></Repetition></LogonTrigger>", 1)
		}, "repeats"},
		{"other user", func(x string) string {
			return strings.Replace(x, `<UserId>PC\Ann</UserId>`, `<UserId>PC\Bob</UserId>`, 1)
		}, "logon trigger"},
		{"elevated", func(x string) string {
			return strings.Replace(x, "</LogonType>", "</LogonType><RunLevel>HighestAvailable</RunLevel>", 1)
		}, "run level"},
		{"password", func(x string) string { return strings.Replace(x, "InteractiveToken", "Password", 1) }, "logon type"},
		{"disabled", func(x string) string {
			return strings.Replace(x, "</Settings>", "<Enabled>false</Enabled></Settings>", 1)
		}, "disabled"},
	} {
		task, err := parseTask([]byte(exported(tc.mut)))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		d := h.taskDiff(task, `PC\Ann`, "S-1-5-21-1-2-3-1001")
		if (tc.want == "") != (d == "") || !strings.Contains(d, tc.want) {
			t.Errorf("%s: diff %q, want %q", tc.name, d, tc.want)
		}
	}
}

func TestEscapeArg(t *testing.T) {
	for in, want := range map[string]string{
		"":                 `""`,
		"plain":            "plain",
		`C:\p\podman.exe`:  `C:\p\podman.exe`,
		`C:\a b\p.exe`:     `"C:\a b\p.exe"`,
		`C:\a b\`:          `"C:\a b\\"`,
		`say "hi"`:         `"say \"hi\""`,
		`x\"y`:             `x\\\"y`,
		"tab\there":        "\"tab\there\"",
		`C:\Program Files`: `"C:\Program Files"`,
	} {
		if got := EscapeArg(in); got != want {
			t.Errorf("EscapeArg(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAutostartTaskRegisters(t *testing.T) {
	registered := ""
	tl := fakeTools(t, "windows", func(c toolCall) reply {
		switch c.args[0] {
		case "read":
			return reply{stdout: registered}
		case "register":
			registered = exported(nil)
		}
		return reply{}
	})
	h := winHost()
	ctx := context.Background()
	a, err := h.Autostart(ctx)
	if err != nil || a != (Autostart{Kind: KindTask, Name: TaskName}) {
		t.Fatalf("absent: %+v, %v", a, err)
	}
	a, err = h.EnsureAutostart(ctx)
	if err != nil || !a.Present || !a.Current {
		t.Fatalf("EnsureAutostart = %+v, %v", a, err)
	}
	want := []string{"powershell.exe read", "powershell.exe read", "powershell.exe register", "powershell.exe read"}
	if !reflect.DeepEqual(tl.names(), want) {
		t.Errorf("calls = %q", tl.names())
	}
	doc, _ := os.ReadFile(tl.stdin)
	if !bytes.Contains(doc, []byte("<Command>conhost.exe</Command>")) || !bytes.Contains(doc, []byte(`<UserId>PC\Ann</UserId>`)) {
		t.Errorf("registered XML:\n%s", doc)
	}

	// Idempotent: a current task is not registered again.
	tl.calls = nil
	if a, err = h.EnsureAutostart(ctx); err != nil || !a.Current {
		t.Fatal(a, err)
	}
	if !reflect.DeepEqual(tl.names(), []string{"powershell.exe read"}) {
		t.Errorf("rerun calls = %q", tl.names())
	}
}

func TestAutostartTaskOutdatedAndRefused(t *testing.T) {
	fakeTools(t, "windows", func(c toolCall) reply {
		switch c.args[0] {
		case "read":
			return reply{stdout: exported(func(x string) string { return strings.Replace(x, `RedHat\Podman`, "Old", 1) })}
		case "register":
			return reply{stderr: "Access is denied. (Exception from HRESULT: 0x80070005)", exit: 1}
		}
		return reply{}
	})
	h := winHost()
	a, err := h.Autostart(context.Background())
	if err != nil || !a.Present || a.Current || !strings.Contains(a.Outdated, "another podman") {
		t.Fatalf("outdated: %+v, %v", a, err)
	}
	_, err = h.EnsureAutostart(context.Background())
	if !errors.Is(err, ErrAutostart) || !strings.Contains(err.Error(), "Access is denied") {
		t.Errorf("refused: %v", err)
	}
}

func TestAutostartRefusesMachineName(t *testing.T) {
	tl := fakeTools(t, "windows", func(toolCall) reply { return reply{} })
	h := &Host{podman: winPodman, Machine: "bad'name"}
	if _, err := h.Autostart(context.Background()); !errors.Is(err, ErrAutostart) {
		t.Errorf("err = %v", err)
	}
	if len(tl.calls) != 0 {
		t.Errorf("ran %q", tl.names())
	}
}

func TestAutostartLinux(t *testing.T) {
	tl := fakeTools(t, "linux", func(toolCall) reply { return reply{} })
	h := &Host{podman: "/usr/bin/podman", Running: true}
	ctx := context.Background()
	a, err := h.Autostart(ctx)
	if err != nil || a != (Autostart{}) {
		t.Errorf("Autostart = %+v, %v", a, err)
	}
	if a, err = h.EnsureAutostart(ctx); err != nil || a.Kind != "" {
		t.Errorf("EnsureAutostart = %+v, %v", a, err)
	}
	if err := h.RemoveAutostart(ctx); err != nil {
		t.Error(err)
	}
	if err := h.StartViaAutostart(ctx); err != nil {
		t.Error(err)
	}
	if len(tl.calls) != 0 {
		t.Errorf("ran %q", tl.names())
	}
}

func TestStartViaTask(t *testing.T) {
	for _, tc := range []struct {
		name, runOut string
		running      bool
		ok           bool
		want         string
	}{
		{"conhost", "4242 conhost\r\n", true, true, ""},
		{"podman", "4242 podman\r\n", true, true, ""},
		{"no pid", "0 \r\n", true, false, "started no conhost.exe"},
		{"other image", "4242 taskhostw\r\n", true, false, "started no conhost.exe"},
		{"not running", "4242 conhost\r\n", false, false, "is not running; run `podman machine start podman-machine-default`"},
	} {
		tl := fakeTools(t, "windows", func(c toolCall) reply {
			if c.name == "powershell.exe" && c.args[0] == "run" {
				return reply{stdout: tc.runOut}
			}
			if c.name == winPodman {
				return reply{stdout: fmt.Sprintf(`[{"Name":"podman-machine-default","Default":true,"Running":%v,"VMType":"wsl"}]`, tc.running)}
			}
			return reply{exit: 9}
		})
		h := winHost()
		err := h.StartViaAutostart(context.Background())
		if tc.ok {
			if err != nil || !h.Running {
				t.Errorf("%s: %v, running %v", tc.name, err, h.Running)
			}
			want := []string{"powershell.exe run", winPodman + " machine list --format json"}
			if !reflect.DeepEqual(tl.names(), want) {
				t.Errorf("%s: calls %q", tc.name, tl.names())
			}
			continue
		}
		if !errors.Is(err, ErrMachineStart) || !strings.Contains(err.Error(), tc.want) || h.Running {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if strings.Contains(tc.want, "started no") && len(tl.calls) != 1 {
			t.Errorf("%s: read the machine after a failed guard: %q", tc.name, tl.names())
		}
	}
}

func TestRemoveTask(t *testing.T) {
	tl := fakeTools(t, "windows", func(toolCall) reply { return reply{} })
	if err := winHost().RemoveAutostart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tl.names(), []string{"powershell.exe delete"}) {
		t.Errorf("calls = %q", tl.names())
	}
}

func TestTaskScriptsAreConstants(t *testing.T) {
	for script := range scriptNames {
		if !strings.Contains(script, "'"+TaskName+"'") || !strings.Contains(script, "exit 1") {
			t.Errorf("script lacks the task name or the error exit:\n%s", script)
		}
	}
	if !strings.Contains(registerTaskScript, "RegisterTask('"+TaskName+"', $xml, 6, $null, $null, 3)") {
		t.Error("register does not use TASK_CREATE_OR_UPDATE and TASK_LOGON_INTERACTIVE_TOKEN")
	}
	if !strings.Contains(runTaskScript, "Wait-Process -Id $id") {
		t.Error("run does not wait for the process")
	}
}

func macHost() *Host {
	return &Host{podman: "/opt/homebrew/bin/podman", Machine: "podman-machine-default", VMType: "applehv"}
}

func TestAgentPlist(t *testing.T) {
	h := &Host{podman: "/Users/a&b/podman", Machine: "m"}
	got := string(h.agentPlist())
	for _, want := range []string{
		"<key>Label</key>\n\t<string>io.github.taco3064.nibfin.podman-machine</string>",
		"<array>\n\t\t<string>/Users/a&amp;b/podman</string>\n\t\t<string>machine</string>\n\t\t<string>start</string>\n\t\t<string>m</string>\n\t</array>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>AbandonProcessGroup</key>\n\t<true/>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plist lacks %q:\n%s", want, got)
		}
	}
}

func TestAutostartAgent(t *testing.T) {
	loaded := false
	tl := fakeTools(t, "darwin", func(c toolCall) reply {
		if c.name != "launchctl" {
			return reply{exit: 9}
		}
		switch c.args[0] {
		case "print":
			if loaded {
				return reply{}
			}
			return reply{exit: 113}
		case "bootstrap":
			loaded = true
		case "bootout":
			loaded = false
		}
		return reply{}
	})
	h := macHost()
	ctx := context.Background()
	a, err := h.Autostart(ctx)
	if err != nil || a != (Autostart{Kind: KindLaunchAgent, Name: AgentLabel}) {
		t.Fatalf("absent: %+v, %v", a, err)
	}
	if a, err = h.EnsureAutostart(ctx); err != nil || !a.Current {
		t.Fatalf("EnsureAutostart = %+v, %v", a, err)
	}
	path, _ := agentPath()
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, h.agentPlist()) {
		t.Errorf("plist = %q, %v", b, err)
	}
	target := "gui/501/" + AgentLabel
	want := []string{
		"launchctl print " + target,
		"launchctl print " + target,
		"launchctl print " + target,
		"launchctl bootstrap gui/501 " + path,
		"launchctl print " + target,
	}
	if !reflect.DeepEqual(tl.names(), want) {
		t.Errorf("calls = %q\nwant %q", tl.names(), want)
	}

	// Idempotent.
	tl.calls = nil
	if a, err = h.EnsureAutostart(ctx); err != nil || !a.Current || len(tl.calls) != 1 {
		t.Errorf("rerun: %+v, %v, %q", a, err, tl.names())
	}

	// A moved podman: the loaded job is booted out before the new one.
	tl.calls = nil
	h.podman = "/usr/local/bin/podman"
	if a, _ = h.Autostart(ctx); a.Current || !a.Present {
		t.Errorf("moved podman: %+v", a)
	}
	tl.calls = nil
	if a, err = h.EnsureAutostart(ctx); err != nil || !a.Current {
		t.Fatal(a, err)
	}
	want = []string{
		"launchctl print " + target,
		"launchctl print " + target,
		"launchctl bootout " + target,
		"launchctl bootstrap gui/501 " + path,
		"launchctl print " + target,
	}
	if !reflect.DeepEqual(tl.names(), want) {
		t.Errorf("update calls = %q", tl.names())
	}

	// Removal: bootout, then the plist.
	tl.calls = nil
	if err := h.RemoveAutostart(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plist left: %v", err)
	}
	if !reflect.DeepEqual(tl.names(), []string{"launchctl print " + target, "launchctl bootout " + target}) {
		t.Errorf("remove calls = %q", tl.names())
	}
	tl.calls = nil
	if err := h.RemoveAutostart(ctx); err != nil || len(tl.calls) != 1 {
		t.Errorf("second remove: %v, %q", err, tl.names())
	}
}

func TestAgentBootstrapFails(t *testing.T) {
	fakeTools(t, "darwin", func(c toolCall) reply {
		if c.args[0] == "bootstrap" {
			return reply{stderr: "Bootstrap failed: 125: Domain does not support specified action", exit: 5}
		}
		return reply{exit: 113}
	})
	_, err := macHost().EnsureAutostart(context.Background())
	if !errors.Is(err, ErrAutostart) || !strings.Contains(err.Error(), "Domain does not support") {
		t.Errorf("err = %v", err)
	}
}

func TestStartViaAgent(t *testing.T) {
	for _, tc := range []struct {
		name, out string
		exit      int
		running   bool
		want      string
	}{
		{"started", "777\n", 0, true, ""},
		{"not running", "777\n", 0, false, "is not running"},
		{"no pid", "", 0, true, "no process id"},
		{"kickstart fails", "", 113, true, "kickstart"},
	} {
		tl := fakeTools(t, "darwin", func(c toolCall) reply {
			if c.name == "launchctl" {
				return reply{stdout: tc.out, stderr: "Could not find service", exit: tc.exit}
			}
			return reply{stdout: fmt.Sprintf(`[{"Name":"podman-machine-default","Default":true,"Running":%v}]`, tc.running)}
		})
		var waited []int
		waitExit = func(_ context.Context, pid int) error { waited = append(waited, pid); return nil }
		h := macHost()
		err := h.StartViaAutostart(context.Background())
		if tc.want == "" {
			if err != nil || !h.Running || !reflect.DeepEqual(waited, []int{777}) {
				t.Errorf("%s: %v, %v, waited %v", tc.name, err, h.Running, waited)
			}
			if tl.names()[0] != "launchctl kickstart -p gui/501/"+AgentLabel {
				t.Errorf("%s: calls %q", tc.name, tl.names())
			}
			continue
		}
		if !errors.Is(err, ErrMachineStart) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestStartViaAutostartRunningIsNoop(t *testing.T) {
	tl := fakeTools(t, "windows", func(toolCall) reply { return reply{} })
	h := winHost()
	h.Running = true
	if err := h.StartViaAutostart(context.Background()); err != nil || len(tl.calls) != 0 {
		t.Errorf("%v, %q", err, tl.names())
	}
}
