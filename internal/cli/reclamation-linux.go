package cli

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type linuxReclamation struct {
	WSL            string `json:"wsl"`
	HostSetting    string `json:"windows_sparse_setting"`
	RootFilesystem string `json:"root_filesystem"`
	Discard        string `json:"root_device_discard"`
	TrimTool       string `json:"fstrim_tool"`
	Guidance       string `json:"guidance,omitempty"`
}

func localLinuxReclamation() *linuxReclamation {
	return observeLinuxReclamation(os.ReadFile, func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
}

func observeLinuxReclamation(read func(string) ([]byte, error),
	tool func(string) bool) *linuxReclamation {
	o := &linuxReclamation{WSL: "unknown", HostSetting: "unavailable",
		RootFilesystem: "unknown", Discard: "unknown", TrimTool: "unavailable"}
	release, releaseErr := read("/proc/sys/kernel/osrelease")
	version, versionErr := read("/proc/version")
	kernel := strings.ToLower(string(release) + " " + string(version))
	if strings.Contains(kernel, "microsoft") {
		o.WSL, o.HostSetting = "observed", "deferred_windows_entry_required"
		o.Guidance = "From Windows run gh nibfin init --repo OWNER/REPO " +
			"--target wsl:NAME; apply with explicit experimental sparse consent. " +
			"Guest-only setup remains usable, including with interop disabled."
	} else if releaseErr == nil && versionErr == nil {
		o.WSL = "not_observed"
	}
	if tool("fstrim") {
		o.TrimTool = "available"
	}
	mounts, err := read("/proc/self/mountinfo")
	if err != nil {
		return o
	}
	var device, filesystem string
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[4] != "/" {
			continue
		}
		if device != "" {
			return o
		} // Multiple root mounts are ambiguous.
		device = fields[2]
		for i := 6; i+2 < len(fields); i++ {
			if fields[i] == "-" {
				filesystem = fields[i+1]
				break
			}
		}
	}
	switch filesystem {
	case "ext4", "xfs", "btrfs", "overlay", "tmpfs":
		o.RootFilesystem = filesystem
	}
	parts := strings.Split(device, ":")
	if len(parts) != 2 {
		return o
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return o
		}
	}
	base := "/sys/dev/block/" + device
	data, err := read(base + "/queue/discard_max_bytes")
	if err != nil {
		partition, partErr := read(base + "/partition")
		number, parseErr := strconv.ParseUint(strings.TrimSpace(string(partition)), 10, 64)
		if partErr != nil || parseErr != nil || number == 0 {
			return o
		}
		data, err = read(base + "/../queue/discard_max_bytes")
	}
	maximum, parseErr := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err == nil && parseErr == nil {
		o.Discard = "unsupported"
		if maximum > 0 {
			o.Discard = "supported"
		}
	}
	return o
}

// Reconstruct from finite facts; guest-provided guidance and paths are not relayed.
func sanitizedReclamation(o *linuxReclamation) *linuxReclamation {
	if o == nil {
		return nil
	}
	clean := &linuxReclamation{WSL: "unknown", HostSetting: "unavailable",
		RootFilesystem: "unknown", Discard: "unknown", TrimTool: "unavailable"}
	if o.WSL == "observed" {
		clean.WSL, clean.HostSetting = "observed", "deferred_windows_entry_required"
	} else if o.WSL == "not_observed" {
		clean.WSL = o.WSL
	}
	switch o.RootFilesystem {
	case "ext4", "xfs", "btrfs", "overlay", "tmpfs":
		clean.RootFilesystem = o.RootFilesystem
	}
	if o.Discard == "supported" || o.Discard == "unsupported" {
		clean.Discard = o.Discard
	}
	if o.TrimTool == "available" {
		clean.TrimTool = "available"
	}
	return clean
}
