package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

//go:embed shared-core-deploy.py
var sharedCoreDeploy string

var hostNodeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func nativeSharedHostSupported() bool { return runtime.GOARCH == "amd64" }

func deploySharedCore(api API, process Process) (string, error) {
	if process == nil || !nativeSharedHostSupported() {
		return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_LINUX_X64")
	}
	pins := Pins()
	archive, err := candidateArchive(api, pins)
	if err != nil {
		return "", err
	}
	result, err := runSharedDeployment(process, archive, pins.Core.ArchiveHash)
	status := strings.TrimSpace(string(result))
	if err != nil || status != "installed" && status != "existing" {
		return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_ADMIN_OR_READBACK")
	}
	if err := verifyPrivilegedCoreTree("/opt/nibfin/core"); err != nil {
		return "", err
	}
	pin, err := os.CreateTemp("", "nibfin-installed-pin-")
	if err != nil {
		return "", err
	}
	defer os.Remove(pin.Name())
	written := json.NewEncoder(pin).Encode(pins.Core)
	if closed := pin.Close(); written != nil || closed != nil {
		return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_READBACK")
	}
	const entry = "/opt/nibfin/core/bin/nibfin-core.mjs"
	out, err := process.Run(sharedPinnedNode, []string{entry,
		"verify", "/opt/nibfin/core", pin.Name()}, nil)
	var receipt struct {
		Status string    `json:"status"`
		Source SourcePin `json:"source"`
	}
	if err != nil || json.Unmarshal(out, &receipt) != nil || receipt.Status != "verified" ||
		receipt.Source.Repository != pins.Core.Repository ||
		receipt.Source.Commit != pins.Core.Commit {
		return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_READBACK")
	}
	return status, nil
}

func runSharedDeployment(
	process Process, archive []byte, values ...string,
) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks("/usr/bin/python3")
	if err != nil || !regexp.MustCompile(`^/usr/bin/python3\.\d+$`).MatchString(resolved) {
		return nil, errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_SYSTEM_PYTHON")
	}
	if err := requireRootOwnedHostPath(resolved); err != nil {
		return nil, err
	}
	args := append([]string{"-I", "-c", sharedCoreDeploy}, values...)
	return runSharedAdministrator(process, resolved, args, archive)
}

func discoverSharedHost(process Process) (*sharedHost, error) {
	const adoption = "/etc/nibfin/host.json"
	if _, err := os.Lstat(adoption); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if !nativeSharedHostSupported() {
		return nil, errors.New("UNSUPPORTED_SHARED_HOST_ARCHITECTURE")
	}
	node, entry, err := trustedSharedCore()
	if err != nil {
		return nil, err
	}
	return readInstalledSharedHost(process, node, entry)
}

func trustedSharedCore() (string, string, error) {
	const core = "/opt/nibfin/core"
	versionFile := core + "/.node-version"
	entry := core + "/bin/nibfin-core.mjs"
	for _, path := range []string{"/etc/nibfin/host.json", versionFile, entry} {
		if err := requireRootOwnedHostPath(path); err != nil {
			return "", "", err
		}
	}
	raw, err := os.ReadFile(versionFile)
	version := strings.TrimSpace(string(raw))
	if err != nil || !hostNodeVersion.MatchString(version) {
		return "", "", errors.New("SHARED_HOST_NODE_VERSION_UNAVAILABLE")
	}
	node := "/opt/nibfin/tools/node-v" + version + "-linux-x64/bin/node"
	if err := requireRootOwnedHostPath(node); err != nil {
		return "", "", err
	}
	return node, entry, nil
}

func installSharedService(process Process, sources []string, start bool) (string, error) {
	var args []string
	if len(sources) != 0 {
		args = append([]string{strconv.Itoa(os.Getuid())}, sources...)
	}
	operation := "install-shared-service-template"
	if start {
		operation = "start-shared-services"
	}
	output, err := runSharedHostAdmin(process, operation, args...)
	if err != nil {
		return "", errors.New("SHARED_SERVICE_INSTALLATION_REQUIRES_READBACK")
	}
	return decodeSharedServiceResult(output, start)
}

func runSharedHostAdmin(
	process Process, operation string, values ...string,
) ([]byte, error) {
	node, entry, err := trustedSharedCore()
	if err != nil {
		return nil, err
	}
	if process == nil {
		return nil, errors.New("SHARED_HOST_ADMINISTRATION_UNAVAILABLE")
	}
	// Imported modules run before Core can validate its own installation pin.
	if err := verifyPrivilegedCoreTree("/opt/nibfin/core"); err != nil {
		return nil, err
	}
	const env = "/usr/bin/env"
	if err := requireRootOwnedHostPath(env); err != nil {
		return nil, err
	}
	args := []string{"-i", "PATH=/usr/bin:/bin", "LANG=C.UTF-8",
		node, entry, operation}
	args = append(args, values...)
	return runSharedAdministrator(process, env, args, nil)
}

func runSharedAdministrator(
	process Process, program string, args []string, input []byte,
) ([]byte, error) {
	if os.Geteuid() == 0 {
		return process.Run(program, args, input)
	}
	const sudo = "/usr/bin/sudo"
	if err := requireRootOwnedHostPath(sudo); err != nil {
		return nil, err
	}
	options := []string{"-n", "--", program}
	// sudo prompts on its controlling terminal and leaves stdin available
	// for the verified archive. Headless calls retain noninteractive behavior.
	if _, native := process.(NativeProcess); native {
		if terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			terminal.Close()
			options = options[1:]
		}
	}
	return process.Run(sudo, append(options, args...), input)
}

func verifyPrivilegedCoreTree(root string) error {
	if err := requireRootOwnedHostPath(root); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("UNTRUSTED_SHARED_HOST_PATH")
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() && !info.Mode().IsRegular() ||
			info.Mode().Perm()&0022 != 0 || info.Sys().(*syscall.Stat_t).Uid != 0 {
			return errors.New("UNTRUSTED_SHARED_HOST_PATH")
		}
		return nil
	})
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
