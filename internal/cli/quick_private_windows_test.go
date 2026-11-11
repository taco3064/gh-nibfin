package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuickWindowsHandoffRejectsForeignReadACL(t *testing.T) {
	directory := filepath.Join(privateRunnerTestRoot(t), "private-handoff")
	if err := quickNewDirectory(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, quickHandoffFile)
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateQuickPaths([]string{directory, path}); err != nil {
		t.Fatal(err)
	}
	const grantRead = `
$ErrorActionPreference = 'Stop'
$PSModuleAutoLoadingPreference = 'None'
` + windowsPermissionInput + `
$security = [IO.File]::GetAccessControl($paths[0])
$everyone = [Security.Principal.SecurityIdentifier]::new('S-1-1-0')
$rule = [Security.AccessControl.FileSystemAccessRule]::new(
  $everyone, 'Read', 'Allow')
$security.AddAccessRule($rule)
[IO.File]::SetAccessControl($paths[0], $security)
`
	if err := runWindowsPermissionCheck(grantRead, []string{path}); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateQuickPaths([]string{directory, path}); err == nil {
		t.Fatal("a foreign read ACL must reject the secret channel")
	}
}
