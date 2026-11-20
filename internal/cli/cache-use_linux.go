package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func privateCacheInfo(info os.FileInfo, directory bool) bool {
	if info == nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return false
	}
	if directory {
		return info.IsDir() && info.Mode().Perm() == 0700
	}
	return info.Mode().IsRegular() && stat.Nlink == 1 &&
		(info.Mode().Perm() == 0600 || info.Mode().Perm() == 0700)
}

func nativeCacheUse(candidate string) string {
	executable, err := os.Executable()
	if err != nil {
		return "unknown_observation"
	}
	self, err := os.Stat(executable)
	if err != nil {
		return "unknown_observation"
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return "unknown_observation"
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		proc := filepath.Join("/proc", entry.Name())
		info, err := os.Stat(proc)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "unknown_observation"
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return "unknown_observation"
		}
		if stat.Uid != uint32(os.Getuid()) {
			data, err := os.ReadFile(filepath.Join(proc, "cmdline"))
			if err != nil && !os.IsNotExist(err) {
				return "unknown_observation"
			}
			if reason := cacheCommandUse(data, candidate); reason != "" {
				return reason
			}
			continue
		}
		other, exeErr := os.Stat(filepath.Join(proc, "exe"))
		if exeErr == nil && os.SameFile(self, other) {
			return "active_use"
		}
		reason := processCacheUse(proc, candidate)
		if reason != "" {
			return reason
		}
	}
	return ""
}

func processCacheUse(proc, candidate string) string {
	data, err := os.ReadFile(filepath.Join(proc, "cmdline"))
	if err != nil {
		if _, statErr := os.Stat(proc); os.IsNotExist(statErr) {
			return ""
		}
		return "unknown_observation"
	}
	if reason := cacheCommandUse(data, candidate); reason != "" {
		return reason
	}
	for _, link := range []string{"cwd", "exe"} {
		target, err := os.Readlink(filepath.Join(proc, link))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "unknown_observation"
		}
		if withinCacheTree(target, candidate) {
			return "active_use"
		}
	}
	fds, err := os.ReadDir(filepath.Join(proc, "fd"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		return "unknown_observation"
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(proc, "fd", fd.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "unknown_observation"
		}
		if withinCacheTree(target, candidate) {
			return "active_use"
		}
	}
	return ""
}

func cacheCommandUse(data []byte, candidate string) string {
	args := strings.Split(string(data), "\x00")
	for _, arg := range args {
		if strings.Contains(arg, candidate) {
			return "active_use"
		}
	}
	if len(args) > 0 {
		program := filepath.Base(args[0])
		// Another CLI can be between selection and spawning its verifier. A Node
		// process can import source after closing its files; fd absence is insufficient.
		if strings.Contains(program, "nibfin") {
			return "active_use"
		}
		if program == "node" || program == "nodejs" {
			return "unknown_observation"
		}
	}
	return ""
}

func withinCacheTree(target, candidate string) bool {
	return target == candidate ||
		strings.HasPrefix(target, candidate+string(os.PathSeparator))
}

func retainedCacheSource(commit string) string {
	config, err := os.UserConfigDir()
	if err != nil {
		return "unknown_observation"
	}
	dir := filepath.Join(config, "nibfin", "install")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return ""
	}
	canonical, canonicalErr := filepath.EvalSymlinks(dir)
	if err != nil || canonicalErr != nil || canonical != dir ||
		!privateCacheInfo(info, true) {
		return "unknown_observation"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "unknown_observation"
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(name, ".json"), 10, 64)
		if err != nil || id <= 0 {
			return "unknown_observation"
		}
		intent, present, err := readManagedIntent(filepath.Join(dir, name))
		if err != nil || !present || intent.Plan.Repository.ID != id ||
			validateProducerPins(intent.Pins) != nil {
			return "unknown_observation"
		}
		// Local intent has no authoritative completion field. Retain it without
		// demanding publication or privileged host readback just for housekeeping.
		if intent.Pins.Core.Commit == commit {
			return "retained_recovery"
		}
	}
	return ""
}
