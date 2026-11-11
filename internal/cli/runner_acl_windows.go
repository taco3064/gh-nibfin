package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	fileSecurityObject = 1
	ownerAndDACL       = 0x5
	allowACE           = 0
	denyACE            = 1
	inheritOnlyACE     = 0x8
	ancestorWriteMask  = 0x100D0040
	runnerWriteMask    = 0x500D0156
)

var (
	windowsSecurity      = syscall.NewLazyDLL("advapi32.dll")
	getPathSecurity      = windowsSecurity.NewProc("GetNamedSecurityInfoW")
	getAccessControlRule = windowsSecurity.NewProc("GetAce")
)

type windowsACL struct {
	revision  uint8
	reserved  uint8
	size      uint16
	count     uint16
	reserved2 uint16
}

type windowsACE struct {
	kind  uint8
	flags uint8
	size  uint16
	mask  uint32
	sid   uint32
}

func trustedRunnerACLAccounts() (map[string]bool, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	current, err := user.User.Sid.String()
	if err != nil {
		return nil, err
	}
	installer, _, _, err := syscall.LookupSID("", `NT SERVICE\TrustedInstaller`)
	if err != nil {
		return nil, err
	}
	installerID, err := installer.String()
	if err != nil {
		return nil, err
	}
	return map[string]bool{
		current: true, installerID: true, "S-1-5-18": true, "S-1-5-32-544": true,
	}, nil
}

func privateWindowsPath(path string, mask uint32, trusted map[string]bool) (bool, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	var owner *syscall.SID
	var dacl *windowsACL
	var descriptor syscall.Handle
	code, _, _ := getPathSecurity.Call(uintptr(unsafe.Pointer(name)),
		fileSecurityObject, ownerAndDACL, uintptr(unsafe.Pointer(&owner)), 0,
		uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&descriptor)))
	if code != 0 {
		return false, syscall.Errno(code)
	}
	defer syscall.LocalFree(descriptor)
	if owner == nil || dacl == nil || dacl.count == 0 {
		return false, nil
	}
	ownerID, err := owner.String()
	if err != nil || !trusted[ownerID] {
		return false, err
	}
	for index := uint16(0); index < dacl.count; index++ {
		var ace *windowsACE
		ok, _, err := getAccessControlRule.Call(uintptr(unsafe.Pointer(dacl)),
			uintptr(index), uintptr(unsafe.Pointer(&ace)))
		if ok == 0 || ace == nil {
			return false, err
		}
		if ace.flags&inheritOnlyACE != 0 || ace.kind == denyACE {
			continue
		}
		if ace.kind != allowACE || ace.size < 16 {
			return false, nil
		}
		if ace.mask&mask != 0 {
			sid := (*syscall.SID)(unsafe.Pointer(&ace.sid))
			id, err := sid.String()
			if err != nil || !trusted[id] {
				return false, err
			}
		}
	}
	return true, nil
}

func requirePrivateRunnerPaths(paths []string) error {
	return requireWindowsPathACL(paths, runnerWriteMask)
}

func requireWindowsPathACL(paths []string, targetMask uint32) error {
	if len(paths) == 0 {
		return errors.New("WINDOWS_ACL_PATH_REQUIRED")
	}
	trusted, err := trustedRunnerACLAccounts()
	if err != nil {
		return fmt.Errorf("WINDOWS_ACL_TRUST_UNAVAILABLE: %w", err)
	}
	check := func(path string, mask uint32, code int) error {
		allowed, err := privateWindowsPath(path, mask, trusted)
		if err != nil {
			return fmt.Errorf("WINDOWS_ACL_CHECK_UNAVAILABLE: %w", err)
		}
		if !allowed {
			return fmt.Errorf("RUNNER_PRIVATE_FILE_PERMISSIONS_REQUIRED_%d", code)
		}
		return nil
	}
	for path := paths[0]; ; {
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		if err := check(parent, ancestorWriteMask, 43); err != nil {
			return err
		}
		path = parent
	}
	for _, path := range paths {
		if err := check(path, targetMask, 44); err != nil {
			return err
		}
	}
	return nil
}
