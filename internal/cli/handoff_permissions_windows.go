package cli

// Runner integrity checks forbid unrelated writers. Bearer capabilities also
// require confidentiality: reject FILE_READ_DATA and GENERIC_READ grants.
func requirePrivateHandoffPaths(paths []string) error {
	return requireWindowsPathACL(paths, runnerWriteMask|0x80000001)
}
