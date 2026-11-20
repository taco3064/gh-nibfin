package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

func sparseRegistryOpen(parent syscall.Handle, name string) (syscall.Handle, error) {
	text, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	err = syscall.RegOpenKeyEx(parent, text, 0, syscall.KEY_READ, &key)
	return key, err
}

func sparseRegistryString(key syscall.Handle, name string) (string, error) {
	text, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	var kind, size uint32
	err = syscall.RegQueryValueEx(key, text, nil, &kind, nil, &size)
	if err != nil {
		return "", err
	}
	if kind != syscall.REG_SZ || size < 2 || size%2 != 0 {
		return "", inventoryError()
	}
	data := make([]uint16, size/2)
	err = syscall.RegQueryValueEx(key, text, nil, &kind,
		(*byte)(unsafe.Pointer(&data[0])), &size)
	if err != nil {
		return "", err
	}
	return syscall.UTF16ToString(data), nil
}

func sparseBacking(name string) (string, string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	root, err := sparseRegistryOpen(syscall.HKEY_CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Lxss`)
	if err != nil {
		return "", "", err
	}
	defer syscall.RegCloseKey(root)
	var binding, path string
	for index := uint32(0); ; index++ {
		// Windows registry key names have a platform maximum of 255 characters.
		var nameBuffer [256]uint16
		size := uint32(len(nameBuffer))
		err := syscall.RegEnumKeyEx(root, index, &nameBuffer[0], &size,
			nil, nil, nil, nil)
		if err == syscall.Errno(259) {
			break
		} // ERROR_NO_MORE_ITEMS
		if err != nil {
			return "", "", err
		}
		id := syscall.UTF16ToString(nameBuffer[:size])
		key, err := sparseRegistryOpen(root, id)
		if err != nil {
			return "", "", err
		}
		distro, readErr := sparseRegistryString(key, "DistributionName")
		if readErr != nil {
			syscall.RegCloseKey(key)
			return "", "", readErr
		}
		if distro != name {
			syscall.RegCloseKey(key)
			continue
		}
		if binding != "" {
			syscall.RegCloseKey(key)
			return "", "", inventoryError()
		}
		base, baseErr := sparseRegistryString(key, "BasePath")
		file, fileErr := sparseRegistryString(key, "VhdFileName")
		syscall.RegCloseKey(key)
		if baseErr != nil || !filepath.IsAbs(base) {
			return "", "", inventoryError()
		}
		if fileErr == syscall.ERROR_FILE_NOT_FOUND {
			// Resolve from the registered directory, never from a guessed Store path.
			entries, err := os.ReadDir(base)
			if err != nil {
				return "", "", err
			}
			for _, entry := range entries {
				if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".vhdx") {
					if file != "" {
						return "", "", inventoryError()
					}
					file = entry.Name()
				}
			}
		} else if fileErr != nil {
			return "", "", fileErr
		}
		if file == "" || filepath.Base(file) != file {
			return "", "", inventoryError()
		}
		path = filepath.Join(base, file)
		binding = id + "|" + path
	}
	if binding == "" {
		return "", "", inventoryError()
	}
	return binding, path, nil
}

func sparseFileMetadata(path string) (string, string, error) {
	text, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", "unknown", err
	}
	handle, err := syscall.CreateFile(text, 0x80,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", "unknown", err
	}
	defer syscall.CloseHandle(handle)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		return "", "unknown", err
	}
	if info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|
		syscall.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return "", "unknown", inventoryError()
	}
	binding := fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber,
		info.FileIndexHigh, info.FileIndexLow)
	setting := "disabled"
	if info.FileAttributes&0x200 != 0 {
		setting = "enabled"
	} // SPARSE_FILE
	return binding, setting, nil
}

func (NativeProcess) ObserveSparse(name string) sparseObservation {
	return inspectSparse(name, sparseBacking, sparseFileMetadata,
		func() ([]byte, error) {
			return (NativeProcess{}).DesktopQuery("wsl.exe", []string{"--help"})
		})
}

func (NativeProcess) EnableSparse(ctx context.Context, name string) string {
	path, err := desktopToolPath("wsl.exe")
	if err != nil {
		return "unavailable"
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd := exec.CommandContext(ctx, path, sparseManageArguments(name)...)
	cmd.Env = desktopEnvironment()
	var output boundedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	return sparseOperationResult(cmd.Run(), output.Bytes())
}
