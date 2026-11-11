package cli

import "strings"

func requirePrivateQuickPaths(paths []string) error {
	// Runner integrity checks reject foreign writes; handoffs also contain a
	// bearer capability, so foreign reads must be rejected, including file ACLs
	// that override an otherwise private parent directory.
	script := strings.Replace(privateRunnerACL, "$writeMask = [int64]0x500D0156",
		"$writeMask = [int64]0xF01F01FF", 1)
	return runWindowsPermissionCheck(script, paths)
}
