package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const windowsPermissionInput = `
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
$assembly = 'System.Runtime.Serialization, Version=4.0.0.0, Culture=neutral, '
$assembly += 'PublicKeyToken=b77a5c561934e089'
$null = [Reflection.Assembly]::Load($assembly)
$bytes = [Text.Encoding]::UTF8.GetBytes([Console]::In.ReadToEnd())
$stream = [IO.MemoryStream]::new($bytes)
try {
  $serializer = [Runtime.Serialization.Json.DataContractJsonSerializer]::new([string[]])
  [string[]]$paths = $serializer.ReadObject($stream)
} finally { $stream.Dispose() }
`

const privateRunnerACL = `
$ErrorActionPreference = 'Stop'
$PSModuleAutoLoadingPreference = 'None'
try {
` + windowsPermissionInput + `
$sidType = [System.Security.Principal.SecurityIdentifier]
$current = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$trusted = @($current, 'S-1-5-18', 'S-1-5-32-544')
$installer = [System.Security.Principal.NTAccount]::new('NT SERVICE\TrustedInstaller')
$trusted += $installer.Translate($sidType).Value
$writeMask = [int64]0x500D0156
function Test-PrivateAccess($path, $mask) {
  if ([IO.Directory]::Exists($path)) {
    $security = [IO.Directory]::GetAccessControl($path)
  } else {
    $security = [IO.File]::GetAccessControl($path)
  }
  if ($security.GetOwner($sidType).Value -notin $trusted) { return $false }
  $rules = $security.GetAccessRules($true, $true, $sidType)
  if ($rules.Count -eq 0) { return $false }
  foreach ($rule in $rules) {
    if (($rule.PropagationFlags -band 2) -ne 0) { continue }
    if ($rule.AccessControlType -ne 'Allow') { continue }
    if (([int64]$rule.FileSystemRights -band $mask) -ne 0 -and
        $rule.IdentityReference.Value -notin $trusted) { return $false }
  }
  return $true
}
$ancestor = [IO.Directory]::GetParent($paths[0])
while ($null -ne $ancestor) {
  if (-not (Test-PrivateAccess $ancestor.FullName ([int64]0x100D0040))) { exit 43 }
  $ancestor = $ancestor.Parent
}
foreach ($path in $paths) {
  if (-not (Test-PrivateAccess $path $writeMask)) { exit 44 }
}
} catch {
  [Console]::Error.Write($_.Exception.GetType().FullName)
  exit 46
}
`

const initializePrivateRunnerACL = `
$ErrorActionPreference = 'Stop'
$PSModuleAutoLoadingPreference = 'None'
try {
` + windowsPermissionInput + `
  $current = [System.Security.Principal.WindowsIdentity]::GetCurrent().User
  $security = [System.Security.AccessControl.DirectorySecurity]::new()
  $security.SetOwner($current)
  $security.SetAccessRuleProtection($true, $false)
  foreach ($sid in @($current.Value, 'S-1-5-18', 'S-1-5-32-544')) {
    $identity = [System.Security.Principal.SecurityIdentifier]::new($sid)
    $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
      $identity, 'FullControl', 'ContainerInherit, ObjectInherit', 'None', 'Allow')
    $security.AddAccessRule($rule)
  }
  [IO.Directory]::SetAccessControl($paths[0], $security)
} catch {
  [Console]::Error.Write($_.Exception.GetType().FullName)
  exit 46
}
`

func initializePrivateRunnerDirectory(directory string) error {
	return runWindowsPermissionCheck(initializePrivateRunnerACL, []string{directory})
}

func requirePrivateRunnerParent(directory string) error {
	return requirePrivateRunnerPaths([]string{filepath.Dir(directory)})
}

func requirePrivateRunnerPaths(paths []string) error {
	return runWindowsPermissionCheck(privateRunnerACL, paths)
}

func runWindowsPermissionCheck(script string, paths []string) error {
	var system [512]uint16
	count, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(
		uintptr(unsafe.Pointer(&system[0])), uintptr(len(system)))
	if count == 0 || count >= uintptr(len(system)) {
		return errors.New("WINDOWS_SYSTEM_DIRECTORY_UNAVAILABLE")
	}
	powershell := filepath.Join(syscall.UTF16ToString(system[:]),
		"WindowsPowerShell", "v1.0", "powershell.exe")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, powershell, "-NoLogo", "-NoProfile",
		"-NonInteractive", "-Command", script)
	data, _ := json.Marshal(paths)
	var diagnostic bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(data), io.Discard, &diagnostic
	cmd.Env = runnerEnvironment(nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if cmd.Run() != nil {
		if cmd.ProcessState == nil {
			return errors.New("WINDOWS_ACL_CHECK_PROCESS_UNAVAILABLE")
		}
		code := cmd.ProcessState.ExitCode()
		if code == 43 || code == 44 || code == 45 {
			return fmt.Errorf("RUNNER_PRIVATE_FILE_PERMISSIONS_REQUIRED_%d", code)
		}
		kind := strings.TrimSpace(diagnostic.String())
		if !regexp.MustCompile(`^[A-Za-z0-9_.]{1,160}$`).MatchString(kind) {
			kind = "UNKNOWN"
		}
		return fmt.Errorf("WINDOWS_ACL_CHECK_UNAVAILABLE_%d_%s", code, kind)
	}
	return nil
}
