package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

const terminalHelperEnv = "NIBFIN_TEST_CLAUDE_TERMINAL_HELPER"

// Runs only as a child of the tests below, which choose its terminal.
// It prints the kind or the refusal code, never the credential value.
func TestClaudeTerminalHelperProcess(t *testing.T) {
	if os.Getenv(terminalHelperEnv) == "" {
		t.Skip("helper process only")
	}
	credential, err := promptRebuildClaudeCredential()
	if err != nil {
		fmt.Print("result=" + err.Error())
		return
	}
	defer credential.clear()
	fmt.Print("result=" + credential.kind)
}

func terminalHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestClaudeTerminalHelperProcess$",
		"-test.count=1")
	cmd.Env = append(os.Environ(), terminalHelperEnv+"=1")
	return cmd
}

func TestClaudeCredentialRefusesWithoutTerminal(t *testing.T) {
	// A new session has no controlling terminal, whatever runs the tests.
	cmd := terminalHelper(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = strings.NewReader(fixtureClaudeKey + "\ny\n")
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output),
		"result=REBUILD_CLAUDE_CREDENTIAL_REQUIRES_TERMINAL") {
		t.Fatal("standard input must never replace the terminal", err, string(output))
	}
}

func fixtureTerminalControl(fd, request uintptr, value unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request,
		uintptr(value)); errno != 0 {
		return errno
	}
	return nil
}

func fixturePseudoTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no pseudo-terminal support", err)
	}
	var unlock int32
	var number uint32
	if fixtureTerminalControl(master.Fd(), syscall.TIOCSPTLCK,
		unsafe.Pointer(&unlock)) != nil || fixtureTerminalControl(master.Fd(),
		syscall.TIOCGPTN, unsafe.Pointer(&number)) != nil {
		master.Close()
		t.Skip("no pseudo-terminal support")
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number),
		os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Skip("no pseudo-terminal support", err)
	}
	return master, slave
}

func TestClaudeCredentialTerminalIsHiddenAndRestored(t *testing.T) {
	master, slave := fixturePseudoTerminal(t)
	defer master.Close()
	var before syscall.Termios
	if terminalAttributes(slave.Fd(), syscall.TCGETS, &before) != nil {
		t.Fatal("pseudo-terminal attributes unavailable")
	}
	chunks := make(chan []byte, 64)
	go func() {
		defer close(chunks)
		for {
			buffer := make([]byte, 1024)
			n, err := master.Read(buffer)
			if n > 0 {
				chunks <- buffer[:n]
			}
			if err != nil {
				return
			}
		}
	}()
	var screen bytes.Buffer
	waitFor := func(text string) {
		for !strings.Contains(screen.String(), text) {
			chunk, ok := <-chunks
			if !ok {
				t.Fatal("terminal closed before", text, screen.String())
			}
			screen.Write(chunk)
		}
	}
	cmd := terminalHelper(t)
	cmd.Stdin = slave
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor("input hidden")
	master.Write([]byte(fixtureClaudeKey + "\r"))
	waitFor("Continue?")
	master.Write([]byte("y\r"))
	if err := cmd.Wait(); err != nil ||
		!strings.Contains(stdout.String(), "result=anthropic-api-key") {
		t.Fatal(err, stdout.String())
	}
	var after syscall.Termios
	if terminalAttributes(slave.Fd(), syscall.TCGETS, &after) != nil ||
		after.Lflag != before.Lflag || after.Cc != before.Cc {
		t.Fatal("terminal mode was not restored")
	}
	slave.Close()
	for chunk := range chunks {
		screen.Write(chunk)
	}
	if strings.Contains(screen.String(), fixtureClaudeKey) ||
		strings.Contains(stdout.String(), fixtureClaudeKey) {
		t.Fatal("credential was echoed or printed")
	}
}

func TestClaudeCredentialValueNeverReachesIntentOrOutput(t *testing.T) {
	d := newRebuildDriver(t)
	d.failure = "start"
	value := "sk-ant-api03-" + strings.Repeat("Q", 40)
	admin := &fixtureCredentialAdmin{results: map[string]string{
		"status":     `{"status":"active","phase":"active","memberId":7}`,
		"disconnect": `{"status":"disconnected"}`,
		"intake":     `{"status":"active","memberId":7}`}}
	run := func(out *bytes.Buffer) error {
		ops := d.operations()
		credential := &claudeCredential{"anthropic-api-key", []byte(value)}
		ops.bindClaudeCredentialAdmin(admin.run, credential)
		return (App{API: d}).applyRebuild(
			d.api.data["repos/example/project"].(Repository),
			d.api.data["user"].(User), "claude", []byte("gho_fixture"),
			json.NewEncoder(out), ops)
	}
	var first, second bytes.Buffer
	if err := run(&first); err == nil {
		t.Fatal("interrupted replacement completed")
	}
	if strings.Join(admin.calls, ",") != "status,disconnect,intake" ||
		!bytes.Contains(admin.input, []byte(value)) {
		t.Fatal("credential must reach Core's pipe once", admin.calls)
	}
	path, _ := sharedMutationPath()
	saved, err := os.ReadFile(path)
	if err != nil || bytes.Contains(saved, []byte(value)) ||
		!bytes.Contains(saved, []byte(`"credentialKind":"anthropic-api-key"`)) {
		t.Fatal("intent must record the kind and never the value", err)
	}
	// No private file the CLI wrote may contain the value.
	err = filepath.WalkDir(os.Getenv("XDG_CONFIG_HOME"),
		func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(name)
			if err == nil && bytes.Contains(data, []byte(value)) {
				t.Fatal("credential written to disk", name)
			}
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	d.failure = ""
	if err := run(&second); err != nil {
		t.Fatal(err, second.String())
	}
	if strings.Contains(first.String()+second.String(), value) {
		t.Fatal("credential in command output")
	}
}
