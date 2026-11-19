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

func nativeSharedHostSupported() bool {
	return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
}

func deploySharedCore(api API, process Process) (string, error) {
	if process == nil || !nativeSharedHostSupported() {
		return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_NATIVE_LINUX")
	}
	selection, err := sharedInstallationFromProducer(api)
	if err != nil {
		return "", err
	}
	return deploySelectedSharedCore(process, selection)
}

func deploySelectedSharedCore(
	process Process, selection sharedInstallation,
) (string, error) {
	pins := Pins()
	archive, target := selection.archive, selection.target
	if err := requireRootOwnedHostPath(target.nodePath()); err != nil {
		return "", err
	}
	verification := verifySharedExecutableFile(target.nodePath(), target.NodeHash, target)
	if verification != nil {
		return "", verification
	}
	result, err := runSharedDeployment(process, archive, pins.Core.ArchiveHash)
	status := strings.TrimSpace(string(result))
	if err != nil || status != "installed" && status != "existing" {
		return "", errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_ADMIN_OR_READBACK")
	}
	if err := verifyPrivilegedCoreTree("/opt/nibfin/core"); err != nil {
		return "", err
	}
	if err := verifyInstalledCoreManifest("/opt/nibfin/core", pins.Core); err != nil {
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
	out, err := process.Run(target.nodePath(), []string{entry,
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
	args := append([]string{"-I", "-c", sharedDeploymentSource(values)}, values...)
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
	if err := requireRootOwnedHostPath(adoption); err != nil {
		return nil, err
	}
	node, entry, err := trustedSharedCore()
	if err != nil {
		return nil, err
	}
	host, err := readInstalledSharedHost(process, node, entry)
	if err != nil {
		return nil, err
	}
	if err := readInstalledAgent(process, host); err != nil {
		return nil, err
	}
	return host, nil
}

func trustedSharedCore() (string, string, error) {
	target, err := trustedSharedTarget()
	if err != nil {
		return "", "", err
	}
	return target.nodePath(), "/opt/nibfin/core/bin/nibfin-core.mjs", nil
}

func adoptedSharedTarget() (*sharedTarget, error) {
	if _, err := os.Lstat("/etc/nibfin/host.json"); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	target, err := trustedSharedTarget()
	if err != nil {
		return nil, err
	}
	return &target, nil
}

func trustedSharedTarget() (sharedTarget, error) {
	const core = "/opt/nibfin/core"
	versionFile := core + "/.node-version"
	entry := core + "/bin/nibfin-core.mjs"
	for _, path := range []string{versionFile, entry} {
		if err := requireRootOwnedHostPath(path); err != nil {
			return sharedTarget{}, err
		}
	}
	if err := verifyPrivilegedCoreTree(core); err != nil {
		return sharedTarget{}, err
	}
	pin, err := installedSharedPin()
	if err != nil {
		return sharedTarget{}, err
	}
	if err := verifyInstalledCoreManifest(core, pin); err != nil {
		return sharedTarget{}, err
	}
	raw, err := boundedLinuxFile(versionFile)
	if err != nil {
		return sharedTarget{}, errors.New("SHARED_HOST_NODE_VERSION_UNAVAILABLE")
	}
	contractPath := core + "/src/shared-platform.json"
	if err := requireRootOwnedHostPath(contractPath); err != nil {
		return sharedTarget{}, err
	}
	data, err := boundedLinuxFile(contractPath)
	if err != nil {
		return sharedTarget{}, err
	}
	contract, err := parseSharedPlatform(data, raw)
	if err != nil {
		return sharedTarget{}, err
	}
	target, err := nativeSharedTarget(contract)
	if err != nil {
		return sharedTarget{}, err
	}
	node := target.nodePath()
	if err := requireRootOwnedHostPath(node); err != nil {
		return sharedTarget{}, err
	}
	if err := verifySharedExecutableFile(node, target.NodeHash, target); err != nil {
		return sharedTarget{}, err
	}
	return target, nil
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
	if err := requireRootOwnedHostPath("/etc/nibfin/host.json"); err != nil {
		return nil, err
	}
	return runInstalledSharedAdmin(process, nil, operation, values...)
}

func adoptInitialSharedHost(
	process Process, plan InitPlan, runners []sharedRegistration,
) (*sharedHost, error) {
	return adoptInitialSharedHostWithPins(process, plan, runners, Pins())
}

func adoptInitialSharedHostWithPins(process Process, plan InitPlan,
	runners []sharedRegistration, pins Producers) (*sharedHost, error) {
	const profile = "/etc/nibfin/shared-host-profile.json"
	if _, err := os.Lstat(profile); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err := requireRootOwnedHostPath(profile); err != nil {
		return nil, err
	}
	if len(runners) != 2 {
		return nil, errors.New("SHARED_HOST_REQUIRES_TWO_STAGED_RUNNERS")
	}
	input, err := json.Marshal(pins.Core)
	if err != nil {
		return nil, err
	}
	ids := strconv.FormatInt(runners[0].ID, 10) + "," + strconv.FormatInt(runners[1].ID, 10)
	_, err = runInstalledSharedAdmin(process, input, "adopt-shared-host",
		plan.Repository.FullName, strconv.FormatInt(plan.Repository.ID, 10),
		strconv.FormatInt(plan.Member.ID, 10), pins.Action.Commit, ids)
	if err != nil {
		return nil, errors.New("SHARED_HOST_ADOPTION_REQUIRES_READBACK")
	}
	host, err := discoverSharedHost(process)
	if err != nil || host == nil || host.RepositoryID != plan.Repository.ID ||
		host.Repository != plan.Repository.FullName || host.MemberID != plan.Member.ID ||
		host.adoptionStatus(plan.Repository.ID) != "observed" ||
		host.Runners[0].ID != runners[0].ID || host.Runners[1].ID != runners[1].ID ||
		host.Runners[0].Name != runners[0].Name || host.Runners[1].Name != runners[1].Name {
		return nil, errors.New("SHARED_HOST_ADOPTION_REQUIRES_READBACK")
	}
	return host, nil
}

func runInstalledSharedAdmin(
	process Process, input []byte, operation string, values ...string,
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
	return runSharedAdministrator(process, env, args, input)
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
	switch process.(type) {
	case NativeProcess, onboardingProcess, coreResultProcess:
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
