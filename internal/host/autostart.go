package host

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The autostart entry that starts the machine at logon (and, on Windows, on
// resume from sleep), so the VM does not depend on the process tree that
// ran gh.
const (
	// TaskName is the per-user Task Scheduler task on Windows, in the root
	// folder.
	TaskName = "Nibfin podman machine"
	// AgentLabel is the per-user launchd LaunchAgent on macOS.
	AgentLabel = "io.github.taco3064.nibfin.podman-machine"

	KindTask        = "task"
	KindLaunchAgent = "launchagent"
)

// Platform lookups of the autostart code; tests replace them.
var (
	homeDir = os.UserHomeDir
	getuid  = os.Getuid
	// currentUser returns the user's account name (DOMAIN\user on Windows)
	// and its id (the SID on Windows).
	currentUser = func() (name, id string, err error) {
		u, err := user.Current()
		if err != nil {
			return "", "", err
		}
		return u.Username, u.Uid, nil
	}
	// waitExit waits until the process pid exits (macOS only).
	waitExit = waitPID
)

var machineName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Autostart is the state of the autostart entry.
type Autostart struct {
	// Kind is KindTask, KindLaunchAgent, or empty where none is needed (the
	// local Linux host, whose runners are systemd user services with
	// linger).
	Kind string `json:"kind"`
	// Name is the task name or the LaunchAgent label.
	Name    string `json:"name,omitempty"`
	Present bool   `json:"present"`
	// Current is true when the registered entry equals the desired one (on
	// macOS also loaded).
	Current bool `json:"current"`
	// Outdated says how a present entry differs from the desired one.
	Outdated string `json:"outdated,omitempty"`
}

// AutostartKind is the kind of entry this host uses.
func (h *Host) AutostartKind() string {
	if h.Machine == "" {
		return ""
	}
	switch goos {
	case "windows":
		return KindTask
	case "darwin":
		return KindLaunchAgent
	}
	return ""
}

// Autostart reads the autostart entry; it never writes.
func (h *Host) Autostart(ctx context.Context) (Autostart, error) {
	a := Autostart{Kind: h.AutostartKind()}
	switch a.Kind {
	case KindTask:
		a.Name = TaskName
	case KindLaunchAgent:
		a.Name = AgentLabel
	default:
		return a, nil
	}
	if err := h.checkName(); err != nil {
		return a, err
	}
	if a.Kind == KindTask {
		return h.readTask(ctx, a)
	}
	return h.readAgent(ctx, a)
}

// EnsureAutostart registers or updates the entry when it is not current,
// then reads it back.
func (h *Host) EnsureAutostart(ctx context.Context) (Autostart, error) {
	a, err := h.Autostart(ctx)
	if err != nil || a.Kind == "" || a.Current {
		return a, err
	}
	if a.Kind == KindTask {
		err = h.registerTask(ctx)
	} else {
		err = h.loadAgent(ctx)
	}
	if err != nil {
		return a, err
	}
	return h.Autostart(ctx)
}

// StartViaAutostart starts the stopped machine through the autostart entry,
// waits for that start to end, and reads the machine's state once. Where no
// entry is needed it is Start.
func (h *Host) StartViaAutostart(ctx context.Context) error {
	if h.Running {
		return nil
	}
	var entry string
	switch h.AutostartKind() {
	case KindTask:
		entry = fmt.Sprintf("task %q", TaskName)
		if err := h.runTask(ctx); err != nil {
			return err
		}
	case KindLaunchAgent:
		entry = "LaunchAgent " + AgentLabel
		if err := h.kickstartAgent(ctx); err != nil {
			return err
		}
	default:
		return h.Start(ctx)
	}
	list, err := h.machines(ctx)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.Name == h.Machine && m.Running {
			h.Running = true
			return nil
		}
	}
	return errorf(ErrMachineStart, "started through %s, but %s is not running; run `podman machine start %s` to see why", entry, h.Machine, h.Machine)
}

// RemoveAutostart removes the entry; an absent entry counts as removed. It
// never stops the machine.
func (h *Host) RemoveAutostart(ctx context.Context) error {
	switch h.AutostartKind() {
	case KindTask:
		_, err := h.taskScript(ctx, "remove the task", deleteTaskScript, nil)
		return err
	case KindLaunchAgent:
		loaded, err := h.agentLoaded(ctx)
		if err != nil {
			return err
		}
		if loaded {
			if err := h.launchctl(ctx, "bootout", agentTarget()); err != nil {
				return err
			}
		}
		path, err := agentPath()
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errorf(ErrAutostart, "remove %s: %v", path, err)
		}
	}
	return nil
}

func (h *Host) checkName() error {
	if !machineName.MatchString(h.Machine) {
		return errorf(ErrAutostart, "the podman machine name %q has characters gh nibfin does not put in an autostart entry; use a name of letters, digits, '.', '_' and '-'", h.Machine)
	}
	return nil
}

// result is one finished child process.
type result struct {
	stdout, stderr string
	exit           int
}

// run runs a host tool with the filtered environment. err is set only when
// the tool could not run or ctx ended; a non-zero exit is in the result.
func run(ctx context.Context, stdin io.Reader, name string, args ...string) (result, error) {
	var stdout, stderr bytes.Buffer
	c := filtered(command(ctx, name, args...))
	c.Stdin = stdin
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if ctx.Err() != nil {
		return result{}, ctx.Err()
	}
	r := result{stdout.String(), strings.TrimSpace(stderr.String()), 0}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		r.exit = ee.ExitCode()
		return r, nil
	}
	return r, err
}

func (r result) message() string {
	if r.stderr != "" {
		return r.stderr
	}
	if s := strings.TrimSpace(r.stdout); s != "" {
		return s
	}
	return "exit status " + strconv.Itoa(r.exit)
}

// ---- Windows: Task Scheduler through its COM API in Windows PowerShell ----

// taskPrelude finds the task; the scripts below are constants and only the
// task XML travels, on stdin. Errors go to stderr with exit status 1.
const taskPrelude = `$ErrorActionPreference = 'Stop'
try {
  $s = New-Object -ComObject Schedule.Service
  $s.Connect()
  $f = $s.GetFolder('\')
  $t = $null
  foreach ($x in $f.GetTasks(1)) { if ($x.Name -eq '` + TaskName + `') { $t = $x } }
`

const taskEnd = `} catch {
  [Console]::Error.WriteLine($_.Exception.Message)
  exit 1
}
`

var (
	// readTaskScript prints the task's XML as UTF-8, nothing when absent.
	readTaskScript = taskPrelude + `  if ($t) {
    $b = [Text.Encoding]::UTF8.GetBytes($t.Xml)
    $o = [Console]::OpenStandardOutput()
    $o.Write($b, 0, $b.Length)
    $o.Flush()
  }
` + taskEnd
	// registerTaskScript registers the task XML read from stdin (UTF-8) as
	// TASK_CREATE_OR_UPDATE with TASK_LOGON_INTERACTIVE_TOKEN.
	registerTaskScript = taskPrelude + `  $i = [Console]::OpenStandardInput()
  $m = New-Object IO.MemoryStream
  $i.CopyTo($m)
  $xml = [Text.Encoding]::UTF8.GetString($m.ToArray())
  $f.RegisterTask('` + TaskName + `', $xml, 6, $null, $null, 3) | Out-Null
` + taskEnd
	// runTaskScript runs the task, prints the started process's id and
	// image name, and waits for that process to exit when it is the
	// task's conhost or podman.
	runTaskScript = taskPrelude + `  if (-not $t) { throw 'the task ` + TaskName + ` is not registered' }
  $r = $t.Run($null)
  $id = 0
  try { $id = [int]$r.EnginePID } catch { }
  $n = ''
  if ($id -ne 0) {
    $p = Get-Process -Id $id -ErrorAction SilentlyContinue
    if ($p) { $n = $p.ProcessName }
  }
  [Console]::Out.WriteLine("$id $n")
  [Console]::Out.Flush()
  if ($n -eq 'conhost' -or $n -eq 'podman') { Wait-Process -Id $id -ErrorAction SilentlyContinue }
` + taskEnd
	// deleteTaskScript deletes the task when it exists.
	deleteTaskScript = taskPrelude + `  if ($t) { $f.DeleteTask('` + TaskName + `', 0) }
` + taskEnd
)

// encodeScript is a script as powershell -EncodedCommand takes it: base64
// of its UTF-16LE text.
func encodeScript(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// taskScript runs one of the task scripts; a failure is ErrAutostart.
func (h *Host) taskScript(ctx context.Context, what, script string, stdin io.Reader) (string, error) {
	r, err := run(ctx, stdin, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodeScript(script))
	if err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		return "", errorf(ErrAutostart, "%s: powershell.exe: %v", what, err)
	}
	if r.exit != 0 {
		return "", errorf(ErrAutostart, "%s %q: %s", what, TaskName, r.message())
	}
	return r.stdout, nil
}

const (
	taskNS = "http://schemas.microsoft.com/windows/2004/02/mit/task"
	// resumeQuery selects resume from sleep in the System log.
	resumeQuery = `<QueryList><Query Id="0" Path="System"><Select Path="System">*[System[Provider[@Name='Microsoft-Windows-Power-Troubleshooter'] and EventID=1]]</Select></Query></QueryList>`
)

type taskXML struct {
	XMLName     xml.Name      `xml:"http://schemas.microsoft.com/windows/2004/02/mit/task Task"`
	Version     string        `xml:"version,attr"`
	Description string        `xml:"RegistrationInfo>Description,omitempty"`
	Triggers    taskTriggers  `xml:"Triggers"`
	Principal   taskPrincipal `xml:"Principals>Principal"`
	Settings    taskSettings  `xml:"Settings"`
	Actions     taskActions   `xml:"Actions"`
}

type anyElement struct {
	XMLName xml.Name
}

type taskTriggers struct {
	Logon []logonTrigger `xml:"LogonTrigger"`
	Event []eventTrigger `xml:"EventTrigger"`
	Other []anyElement   `xml:",any"`
}

type logonTrigger struct {
	Enabled    *bool       `xml:"Enabled"`
	UserID     string      `xml:"UserId"`
	Repetition *anyElement `xml:"Repetition"`
}

type eventTrigger struct {
	Enabled      *bool       `xml:"Enabled"`
	Subscription string      `xml:"Subscription"`
	Repetition   *anyElement `xml:"Repetition"`
}

type taskPrincipal struct {
	ID        string  `xml:"id,attr"`
	UserID    string  `xml:"UserId,omitempty"`
	LogonType string  `xml:"LogonType"`
	RunLevel  *string `xml:"RunLevel"`
}

type taskSettings struct {
	MultipleInstancesPolicy    *string `xml:"MultipleInstancesPolicy"`
	DisallowStartIfOnBatteries *bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     *bool   `xml:"StopIfGoingOnBatteries"`
	AllowStartOnDemand         *bool   `xml:"AllowStartOnDemand"`
	Enabled                    *bool   `xml:"Enabled"`
	ExecutionTimeLimit         *string `xml:"ExecutionTimeLimit"`
}

type taskActions struct {
	Context string       `xml:"Context,attr,omitempty"`
	Exec    []execAction `xml:"Exec"`
	Other   []anyElement `xml:",any"`
}

type execAction struct {
	Command   string `xml:"Command"`
	Arguments string `xml:"Arguments"`
}

func ptr[T any](v T) *T { return &v }

// taskArguments are conhost's arguments: it runs podman machine start
// without a window.
func (h *Host) taskArguments() string {
	return "--headless " + EscapeArg(h.podman) + " machine start " + EscapeArg(h.Machine)
}

// desiredTask is the task runner add registers for the user.
func (h *Host) desiredTask(userName string) taskXML {
	return taskXML{
		Version:     "1.2",
		Description: "Starts podman machine " + h.Machine + " at logon and on resume from sleep, so the Nibfin runners in it stay up (gh nibfin runner add).",
		Triggers: taskTriggers{
			Logon: []logonTrigger{{Enabled: ptr(true), UserID: userName}},
			Event: []eventTrigger{{Enabled: ptr(true), Subscription: resumeQuery}},
		},
		Principal: taskPrincipal{ID: "Author", UserID: userName, LogonType: "InteractiveToken", RunLevel: ptr("LeastPrivilege")},
		Settings: taskSettings{
			MultipleInstancesPolicy:    ptr("IgnoreNew"),
			DisallowStartIfOnBatteries: ptr(false),
			StopIfGoingOnBatteries:     ptr(false),
			AllowStartOnDemand:         ptr(true),
			Enabled:                    ptr(true),
			ExecutionTimeLimit:         ptr("PT0S"),
		},
		Actions: taskActions{Context: "Author", Exec: []execAction{{Command: "conhost.exe", Arguments: h.taskArguments()}}},
	}
}

func (h *Host) taskDocument(userName string) ([]byte, error) {
	b, err := xml.MarshalIndent(h.desiredTask(userName), "", "  ")
	if err != nil {
		return nil, errorf(ErrAutostart, "build the task XML: %v", err)
	}
	// No declaration: the scheduler gets the document as a string.
	return b, nil
}

func or[T comparable](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// taskDiff compares a registered task with the desired one, with the
// schema's defaults for absent elements; "" means equal. The user may appear
// by name or by SID.
func (h *Host) taskDiff(t taskXML, userName, userID string) string {
	sameUser := func(u string) bool {
		return strings.EqualFold(u, userName) || (userID != "" && strings.EqualFold(u, userID))
	}
	tr := t.Triggers
	switch {
	case len(tr.Logon) != 1 || len(tr.Event) != 1 || len(tr.Other) != 0:
		return "the triggers are not one logon and one resume trigger"
	case !or(tr.Logon[0].Enabled, true) || !or(tr.Event[0].Enabled, true):
		return "a trigger is disabled"
	case !sameUser(tr.Logon[0].UserID):
		return "the logon trigger is not for " + userName
	case strings.TrimSpace(tr.Event[0].Subscription) != resumeQuery:
		return "the resume trigger has another event query"
	case tr.Logon[0].Repetition != nil || tr.Event[0].Repetition != nil:
		return "a trigger repeats"
	}
	p := t.Principal
	switch {
	case p.LogonType != "InteractiveToken":
		return "the logon type is not InteractiveToken"
	case or(p.RunLevel, "LeastPrivilege") != "LeastPrivilege":
		return "the run level is not the lowest"
	case p.UserID != "" && !sameUser(p.UserID):
		return "the task runs as another user"
	}
	s := t.Settings
	switch {
	case or(s.MultipleInstancesPolicy, "IgnoreNew") != "IgnoreNew":
		return "a new instance is not ignored while one runs"
	case or(s.DisallowStartIfOnBatteries, true), or(s.StopIfGoingOnBatteries, true):
		return "the task does not run on battery"
	case !or(s.AllowStartOnDemand, true):
		return "the task cannot be started on demand"
	case !or(s.Enabled, true):
		return "the task is disabled"
	case or(s.ExecutionTimeLimit, "PT72H") != "PT0S":
		return "the task has a time limit"
	}
	a := t.Actions
	switch {
	case len(a.Exec) != 1 || len(a.Other) != 0:
		return "the task does not have exactly one program to run"
	case !strings.EqualFold(a.Exec[0].Command, "conhost.exe"):
		return "the task runs another program"
	case a.Exec[0].Arguments != h.taskArguments():
		return "the task runs another podman or machine"
	}
	return ""
}

func parseTask(b []byte) (taskXML, error) {
	var t taskXML
	d := xml.NewDecoder(bytes.NewReader(b))
	// The XML arrives as UTF-8 but declares the UTF-16 it has inside the
	// scheduler.
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	err := d.Decode(&t)
	return t, err
}

func (h *Host) readTask(ctx context.Context, a Autostart) (Autostart, error) {
	out, err := h.taskScript(ctx, "read the task", readTaskScript, nil)
	if err != nil || strings.TrimSpace(out) == "" {
		return a, err
	}
	a.Present = true
	name, id, err := currentUser()
	if err != nil {
		return a, errorf(ErrAutostart, "read the current user: %v", err)
	}
	t, err := parseTask([]byte(out))
	if err != nil {
		a.Outdated = "its XML cannot be read: " + err.Error()
		return a, nil
	}
	a.Outdated = h.taskDiff(t, name, id)
	a.Current = a.Outdated == ""
	return a, nil
}

func (h *Host) registerTask(ctx context.Context) error {
	name, _, err := currentUser()
	if err != nil {
		return errorf(ErrAutostart, "read the current user: %v", err)
	}
	doc, err := h.taskDocument(name)
	if err != nil {
		return err
	}
	_, err = h.taskScript(ctx, "register the task", registerTaskScript, bytes.NewReader(doc))
	return err
}

func (h *Host) runTask(ctx context.Context) error {
	r, err := run(ctx, nil, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodeScript(runTaskScript))
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return errorf(ErrMachineStart, "run the task %q: powershell.exe: %v", TaskName, err)
	}
	if r.exit != 0 {
		return errorf(ErrMachineStart, "run the task %q: %s", TaskName, r.message())
	}
	id, image, _ := strings.Cut(strings.TrimSpace(firstLine(r.stdout)), " ")
	pid, _ := strconv.Atoi(id)
	image = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(image)), ".exe")
	if pid == 0 || (image != "conhost" && image != "podman") {
		return errorf(ErrMachineStart, "the task %q started no conhost.exe or podman.exe process to wait for (process %q); check the task in Task Scheduler, or run `podman machine start %s` to see why", TaskName, strings.TrimSpace(r.stdout), h.Machine)
	}
	return nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(l, "\r")
}

// EscapeArg quotes s as one argument of a Windows command line, by the rules
// CommandLineToArgvW parses (as syscall.EscapeArg does on Windows).
func EscapeArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, "\"\\ \t") {
		return s
	}
	hasSpace := strings.ContainsAny(s, " \t")
	if !strings.ContainsAny(s, `"\`) {
		return `"` + s + `"`
	}
	var b strings.Builder
	if hasSpace {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
		default:
			slashes = 0
		}
		b.WriteByte(c)
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			b.WriteByte('\\')
		}
		b.WriteByte('"')
	}
	return b.String()
}

// ---- macOS: a launchd LaunchAgent ----

func agentPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", errorf(ErrAutostart, "find the home directory: %v", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist"), nil
}

func agentDomain() string { return "gui/" + strconv.Itoa(getuid()) }
func agentTarget() string { return agentDomain() + "/" + AgentLabel }

func plistString(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return "<string>" + b.String() + "</string>"
}

// agentPlist is the LaunchAgent: podman machine start at load (login), and
// launchd leaves the VM's helper processes running when the start exits.
func (h *Host) agentPlist() []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	` + plistString(AgentLabel) + `
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range []string{h.podman, "machine", "start", h.Machine} {
		b.WriteString("\t\t" + plistString(a) + "\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>AbandonProcessGroup</key>
	<true/>
</dict>
</plist>
`)
	return []byte(b.String())
}

// launchctl runs launchctl; a non-zero exit is ErrAutostart.
func (h *Host) launchctl(ctx context.Context, args ...string) error {
	r, err := run(ctx, nil, "launchctl", args...)
	if err == nil && r.exit != 0 {
		err = errors.New(r.message())
	}
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return errorf(ErrAutostart, "launchctl %s: %v", strings.Join(args, " "), err)
	}
	return nil
}

// agentLoaded reports whether launchd has the job; only launchctl print's
// exit status is used.
func (h *Host) agentLoaded(ctx context.Context) (bool, error) {
	r, err := run(ctx, nil, "launchctl", "print", agentTarget())
	if err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		return false, errorf(ErrAutostart, "launchctl print: %v", err)
	}
	return r.exit == 0, nil
}

func (h *Host) readAgent(ctx context.Context, a Autostart) (Autostart, error) {
	path, err := agentPath()
	if err != nil {
		return a, err
	}
	have, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return a, errorf(ErrAutostart, "read %s: %v", path, err)
	}
	loaded, lerr := h.agentLoaded(ctx)
	if lerr != nil {
		return a, lerr
	}
	a.Present = err == nil || loaded
	switch {
	case !a.Present:
	case err != nil:
		a.Outdated = path + " is missing"
	case !bytes.Equal(have, h.agentPlist()):
		a.Outdated = path + " differs"
	case !loaded:
		a.Outdated = "launchd has not loaded it"
	}
	a.Current = a.Present && a.Outdated == ""
	return a, nil
}

// loadAgent writes the plist when it differs and (re)loads the job.
func (h *Host) loadAgent(ctx context.Context) error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	want := h.agentPlist()
	have, _ := os.ReadFile(path)
	loaded, err := h.agentLoaded(ctx)
	if err != nil {
		return err
	}
	if bytes.Equal(have, want) && loaded {
		return nil
	}
	if !bytes.Equal(have, want) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return errorf(ErrAutostart, "create %s: %v", filepath.Dir(path), err)
		}
		if err := writeAtomic(path, want, 0o644); err != nil {
			return err
		}
		if loaded {
			if err := h.launchctl(ctx, "bootout", agentTarget()); err != nil {
				return err
			}
		}
	}
	return h.launchctl(ctx, "bootstrap", agentDomain(), path)
}

func (h *Host) kickstartAgent(ctx context.Context) error {
	r, err := run(ctx, nil, "launchctl", "kickstart", "-p", agentTarget())
	if err == nil && r.exit != 0 {
		err = errors.New(r.message())
	}
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return errorf(ErrMachineStart, "launchctl kickstart %s: %v", agentTarget(), err)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(firstLine(r.stdout)))
	if perr != nil || pid <= 0 {
		return errorf(ErrMachineStart, "launchctl kickstart printed no process id: %q", strings.TrimSpace(r.stdout))
	}
	if err := waitExit(ctx, pid); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errorf(ErrMachineStart, "wait for LaunchAgent %s (process %d): %v", AgentLabel, pid, err)
	}
	return nil
}

// writeAtomic writes a temp file in path's directory and renames it over
// path, so a crash leaves the old or the new file.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return errorf(ErrAutostart, "write %s: %v", path, err)
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, mode)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return errorf(ErrAutostart, "write %s: %v", path, err)
	}
	return nil
}
