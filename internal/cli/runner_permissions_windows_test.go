package cli

import (
	"path/filepath"
	"testing"
)

func TestRunnerRejectsIntermediateDirectoryDeletePermission(t *testing.T) {
	app, api, host, directory := runnerFixture(t)
	if _, err := runRunnerTest(app, "create", directory, true); err != nil {
		t.Fatal(err)
	}
	const grantUnsafeDelete = `
$ErrorActionPreference = 'Stop'
$PSModuleAutoLoadingPreference = 'None'
` + windowsPermissionInput + `
$security = [IO.Directory]::GetAccessControl($paths[0])
$everyone = [Security.Principal.SecurityIdentifier]::new('S-1-1-0')
$rule = [Security.AccessControl.FileSystemAccessRule]::new(
  $everyone, 'DeleteSubdirectoriesAndFiles', 'None', 'None', 'Allow')
$security.AddAccessRule($rule)
[IO.Directory]::SetAccessControl($paths[0], $security)
`
	if err := runWindowsPermissionCheck(grantUnsafeDelete,
		[]string{filepath.Join(directory, "bin")}); err != nil {
		t.Fatal(err)
	}
	if _, err := runRunnerTest(app, "register", directory, true); err == nil {
		t.Fatal("intermediate directory replacement permission must deny registration")
	}
	if api.tokens != 0 || host.calls != 0 {
		t.Fatal("unsafe directory must be rejected before token issue or configuration")
	}
}
