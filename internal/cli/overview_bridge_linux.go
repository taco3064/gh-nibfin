package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

func osOverviewInput() io.Reader { return os.Stdin }

func loadOverviewGuest() (overviewBinding, string, string, error) {
	fail := errors.New("GUEST_OVERVIEW_UNAVAILABLE")
	var binding overviewBinding
	if !nativeSharedHostSupported() || linuxIdentity() == "" ||
		requireRootOwnedHostPath("/etc/nibfin/host.json") != nil {
		return binding, "", "", fail
	}
	entry, err := os.Executable()
	if err != nil {
		return binding, "", "", fail
	}
	entry, err = toolPath(entry)
	if err != nil || requireOverviewOwner(entry) != nil {
		return binding, "", "", fail
	}
	stat, err := os.Stat(entry)
	if err != nil {
		return binding, "", "", fail
	}
	s := stat.Sys().(*syscall.Stat_t)
	stamp := fmt.Sprintf("%d:%d:%d:%d:%d", s.Dev, s.Ino, s.Size,
		s.Mtim.Sec, s.Mtim.Nsec)
	node, core, err := trustedSharedCore()
	binding = overviewBinding{Schema: 1, Identity: linuxIdentity(),
		Architecture: runtime.GOARCH, Entry: entry, Stamp: stamp}
	return binding, node, core, err
}

func requireOverviewOwner(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return errors.New("UNTRUSTED_TOOL_PATH")
		}
		uid := info.Sys().(*syscall.Stat_t).Uid
		if uid != 0 && uid != uint32(os.Getuid()) || info.Mode().Perm()&0022 != 0 {
			return errors.New("UNTRUSTED_TOOL_PATH")
		}
		if current == "/" {
			return nil
		}
	}
}
