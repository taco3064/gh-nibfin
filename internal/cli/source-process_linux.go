package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func sourceFileIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) ||
		stat.Nlink != 1 && !info.IsDir() {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}

func sourceProcessStart(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// comm may itself contain spaces and parentheses. Field 22 is starttime.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", errors.New("process identity unreadable")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return "", errors.New("process identity unreadable")
	}
	return fields[19], nil
}

func currentSourceOwner() sourceOwner {
	boot, bootErr := os.ReadFile("/proc/sys/kernel/random/boot_id")
	start, startErr := sourceProcessStart(os.Getpid())
	namespace, nsErr := os.Readlink("/proc/self/ns/pid")
	if bootErr != nil || startErr != nil || nsErr != nil {
		return sourceOwner{}
	}
	return sourceOwner{os.Getpid(), start, strings.TrimSpace(string(boot)) + namespace}
}

func observeSourceUse(record sourceRecord) string {
	current := currentSourceOwner()
	if current.Boot == "" || !validSourceOwner(record.Owner) {
		return "process identity observation unavailable; retry on the owning host"
	}
	if record.Owner.Boot != current.Boot {
		// A different namespace/boot is not proof that a mounted source's original
		// consumers are gone. Do not infer termination from PID absence here.
		return "owner boot or process namespace differs; original use is unknown"
	}
	if record.Phase == "starting" || record.Phase == "extracting" {
		return "interrupted setup has no confirmed consumer observation"
	}
	if record.Phase != "released" {
		start, err := sourceProcessStart(record.Owner.PID)
		if err == nil && start == record.Owner.Start {
			return "owning operation is still running"
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "owner termination observation unavailable"
		}
		if record.Phase != "verified" && record.Phase != "verifying" &&
			record.Phase != "prepared" {
			return "unclassified operation phase"
		}
	}
	if record.Group < 0 {
		return "verifier consumer observation unavailable"
	}
	if record.Group > 0 {
		// The dedicated verifier group includes surviving ordinary descendants.
		// An extant/reused group is retained, never attributed to a dead PID.
		err := syscall.Kill(-record.Group, 0)
		if err == nil || errors.Is(err, syscall.EPERM) {
			return "verifier process group survives or its identity was reused"
		}
		if !errors.Is(err, syscall.ESRCH) {
			return "verifier process group observation unavailable"
		}
	}
	return ""
}

// Only the authenticated verifier uses this dedicated group; the cache source
// is never handed to a daemon or a new selector. Wait alone is insufficient:
// retirement also observes the group after the direct verifier has exited.
func (NativeProcess) runSourceVerifier(name string, args []string,
	started func(int) error) ([]byte, error) {
	launched := false
	defer func() {
		if !launched {
			_ = started(0) // No verifier was launched on this path.
		}
	}()
	path, err := toolPath(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), nativeProcessTimeout)
	defer cancel()
	cmd := nativeCommand(ctx, name, path, args, nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	launched = true
	if err := started(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, errors.New("PROCESS_RESULT_UNAVAILABLE")
	}
	return out.Bytes(), nil
}

func (p sharedNodeProcess) runSourceVerifier(name string, args []string,
	started func(int) error) ([]byte, error) {
	if name == "node" {
		name = p.node
	}
	if native, ok := p.Process.(sourceVerifierProcess); ok {
		return native.runSourceVerifier(name, args, started)
	}
	return nil, errors.New("SOURCE_VERIFIER_PROCESS_OBSERVATION_UNAVAILABLE")
}

// Keep numeric PID parsing in one place for persisted owner validation.
func validSourceOwner(owner sourceOwner) bool {
	_, err := strconv.ParseUint(owner.Start, 10, 64)
	return owner.PID > 0 && err == nil && owner.Boot != ""
}
