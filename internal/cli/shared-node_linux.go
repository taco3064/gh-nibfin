package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"syscall"
	"time"
)

func prepareSharedNode(api API, process Process) (Process, error) {
	adopted, err := adoptedSharedTarget()
	if err != nil {
		return nil, err
	}
	if adopted != nil {
		return prepareSelectedSharedNode(process, *adopted)
	}
	selection, err := sharedInstallationFromProducer(api)
	if err != nil {
		return nil, err
	}
	if err := requireManagedLinux(selection.platform, process); err != nil {
		return nil, err
	}
	return prepareSelectedSharedNode(process, selection.target)
}

func prepareSelectedSharedNode(process Process, target sharedTarget) (Process, error) {
	if process == nil || !nativeSharedHostSupported() {
		return nil, errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_NATIVE_LINUX")
	}
	path := target.nodePath()
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		archive, err := downloadSharedNode(target)
		if err != nil {
			return nil, err
		}
		_, err = runSharedDeployment(process, archive, "node", target.NodeArchiveHash,
			target.NodeHash, target.nodeDirectory(), target.Arch)
		if err != nil {
			return nil, errors.New("SHARED_NODE_DEPLOYMENT_REQUIRES_ADMIN_OR_READBACK")
		}
	}
	if err := requireRootOwnedHostPath(path); err != nil {
		return nil, err
	}
	if err := verifySharedExecutableFile(path, target.NodeHash, target); err != nil {
		return nil, err
	}
	return sharedNodeProcess{Process: process, node: path}, nil
}

func verifySharedExecutableFile(path, digest string, target sharedTarget) error {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("SHARED_EXECUTABLE_UNAVAILABLE")
	}
	defer file.Close()
	stat, err := file.Stat()
	const max = 256 * 1024 * 1024
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > max ||
		stat.Mode().Perm()&0111 == 0 || stat.Sys().(*syscall.Stat_t).Nlink != 1 {
		return errors.New("INVALID_SHARED_EXECUTABLE")
	}
	header := make([]byte, 64)
	if _, err := io.ReadFull(file, header); err != nil {
		return errors.New("INVALID_SHARED_EXECUTABLE")
	}
	machine, err := sharedELFMachine(header)
	if err != nil || machine != target.ELFMachine {
		return errors.New("SHARED_PLATFORM_MISMATCH")
	}
	hash := sha256.New()
	hash.Write(header)
	size, err := io.Copy(hash, io.LimitReader(file, max+1))
	if err != nil || size+64 != stat.Size() || hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("SHARED_ARTIFACT_DIGEST_MISMATCH")
	}
	return nil
}

func downloadSharedNode(target sharedTarget) ([]byte, error) {
	client := http.Client{Timeout: 2 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("UNTRUSTED_NODE_DOWNLOAD_REDIRECT")
		}}
	response, err := client.Get(target.nodeURL())
	if err != nil {
		return nil, errors.New("OFFICIAL_NODE_DOWNLOAD_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("OFFICIAL_NODE_DOWNLOAD_UNAVAILABLE")
	}
	const max = 64 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil || len(data) > max || sha256Hash(data) != target.NodeArchiveHash {
		return nil, errors.New("OFFICIAL_NODE_ARCHIVE_MISMATCH")
	}
	return data, nil
}

type sharedNodeProcess struct {
	Process
	node string
}

func (p sharedNodeProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	if name == "node" {
		name = p.node
	}
	return p.Process.Run(name, args, input)
}
