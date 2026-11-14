package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

var hostNodeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func nativeSharedHostSupported() bool { return runtime.GOARCH == "amd64" }

func discoverSharedHost(process Process) (*sharedHost, error) {
	const adoption = "/etc/nibfin/host.json"
	if _, err := os.Lstat(adoption); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if !nativeSharedHostSupported() {
		return nil, errors.New("UNSUPPORTED_SHARED_HOST_ARCHITECTURE")
	}
	const core = "/opt/nibfin/core"
	versionFile := core + "/.node-version"
	entry := core + "/bin/nibfin-core.mjs"
	for _, path := range []string{adoption, versionFile, entry} {
		if err := requireRootOwnedHostPath(path); err != nil {
			return nil, err
		}
	}
	raw, err := os.ReadFile(versionFile)
	version := strings.TrimSpace(string(raw))
	if err != nil || !hostNodeVersion.MatchString(version) {
		return nil, errors.New("SHARED_HOST_NODE_VERSION_UNAVAILABLE")
	}
	node := "/opt/nibfin/tools/node-v" + version + "-linux-x64/bin/node"
	if err := requireRootOwnedHostPath(node); err != nil {
		return nil, err
	}
	if process == nil {
		return nil, errors.New("SHARED_HOST_INSPECTION_UNAVAILABLE")
	}
	output, err := process.Run(node, []string{entry, "inspect-shared-host"}, nil)
	if err != nil {
		return nil, errors.New("SHARED_HOST_INSPECTION_UNAVAILABLE")
	}
	return decodeSharedHost(output)
}

func requireRootOwnedHostPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 ||
			info.Mode().Perm()&0022 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
			return errors.New("UNTRUSTED_SHARED_HOST_PATH")
		}
		if current == "/" {
			return nil
		}
	}
}
