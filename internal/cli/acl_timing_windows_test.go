package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestWindowsACLProbeTiming(t *testing.T) {
	root := privateRunnerTestRoot(t)
	script := "$timer = [Diagnostics.Stopwatch]::StartNew(); " +
		"[Console]::WriteLine('script_start=' + $timer.ElapsedMilliseconds)\n" +
		privateRunnerACL
	for _, phase := range []struct{ marker, name string }{
		{"Import-Module (Join-Path $PSHOME 'Modules\\Microsoft.PowerShell.Utility')",
			"module_import"},
		{"$trusted += $installer.Translate($sidType).Value", "installer_lookup"},
		{"foreach ($path in $paths) {", "ancestors_checked"},
	} {
		stamp := "\n[Console]::WriteLine('" + phase.name + "=' + " +
			"$timer.ElapsedMilliseconds)\n"
		script = strings.Replace(script, phase.marker, phase.marker+stamp, 1)
	}
	script += "\n[Console]::WriteLine('complete=' + $timer.ElapsedMilliseconds)"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	path := filepath.Join(os.Getenv("SystemRoot"), "System32",
		"WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, path, "-NoLogo", "-NoProfile",
		"-NonInteractive", "-Command", script)
	data, _ := json.Marshal([]string{root})
	var output bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Env = bytes.NewReader(data), &output, runnerEnvironment(nil)
	start := time.Now()
	if cmd.Run() != nil {
		t.Fatal("bounded ACL timing probe failed")
	}
	t.Logf("total_milliseconds=%d", time.Since(start).Milliseconds())
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		line = strings.TrimSpace(line)
		if !regexp.MustCompile(`^[a-z_]+=[0-9]+$`).MatchString(line) {
			t.Fatal("timing output must contain only fixed phase names and durations")
		}
		t.Log(line)
	}
}
