package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

func localNTFS(path string) bool {
	input, err := syscall.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return false
	}
	var volume, format [512]uint16
	getVolume := kernel32.NewProc("GetVolumePathNameW")
	ok, _, _ := getVolume.Call(uintptr(unsafe.Pointer(input)),
		uintptr(unsafe.Pointer(&volume[0])), uintptr(len(volume)))
	if ok == 0 {
		return false
	}
	driveType, _, _ := kernel32.NewProc("GetDriveTypeW").Call(
		uintptr(unsafe.Pointer(&volume[0])))
	if driveType != 3 { // DRIVE_FIXED; do not infer remote/removable durability.
		return false
	}
	ok, _, _ = kernel32.NewProc("GetVolumeInformationW").Call(
		uintptr(unsafe.Pointer(&volume[0])), 0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&format[0])), uintptr(len(format)))
	return ok != 0 && syscall.UTF16ToString(format[:]) == "NTFS"
}

func writeDurableState(path string, data []byte) error {
	if !localNTFS(path) {
		return errors.New("LOCAL_NTFS_RUNNER_STATE_REQUIRED")
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return errors.New("INVALID_RUNNER_STATE_PATH")
	}
	const writeThrough = 0x80000000
	const openReparsePoint = 0x00200000
	handle, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ,
		nil, syscall.CREATE_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|writeThrough|openReparsePoint, 0)
	if err != nil {
		return errors.New("RUNNER_STATE_WRITE_UNAVAILABLE")
	}
	file := os.NewFile(uintptr(handle), path)
	_, writeErr := file.Write(data)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("RUNNER_STATE_WRITE_OR_SYNC_FAILED")
	}
	// Windows write-through flushes NTFS metadata; Sync additionally requests
	// FlushFileBuffers. No directory replacement or unsupported dir Sync is used.
	return nil
}
