package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	sharedPinnedNode = "/opt/nibfin/tools/node-v24.22.1-linux-x64/bin/node"
	nodeOfficialURL  = "https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-x64.tar.gz"
	nodeArchiveHash  = "eb6728e77c576a35162c122c45ccadf224d89ff00abc8dae46cc2897c6002d7c"
	nodeBinaryHash   = "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb"
	nodeBinarySize   = 126879696
)

func prepareSharedNode(process Process) (Process, error) {
	if process == nil || !nativeSharedHostSupported() {
		return nil, errors.New("SHARED_CORE_DEPLOYMENT_REQUIRES_LINUX_X64")
	}
	if _, err := os.Lstat(sharedPinnedNode); errors.Is(err, os.ErrNotExist) {
		archive, err := downloadSharedNode()
		if err != nil {
			return nil, err
		}
		if _, err := runSharedDeployment(process, archive,
			"node", nodeArchiveHash, nodeBinaryHash); err != nil {
			return nil, errors.New("SHARED_NODE_DEPLOYMENT_REQUIRES_ADMIN_OR_READBACK")
		}
	}
	if err := requireRootOwnedHostPath(sharedPinnedNode); err != nil {
		return nil, err
	}
	file, err := os.Open(sharedPinnedNode)
	if err != nil {
		return nil, errors.New("SHARED_NODE_DEPLOYMENT_REQUIRES_READBACK")
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, nodeBinarySize+1))
	if err != nil || size != nodeBinarySize || hex.EncodeToString(hash.Sum(nil)) !=
		nodeBinaryHash {
		return nil, errors.New("SHARED_NODE_SOURCE_MISMATCH")
	}
	return sharedNodeProcess{Process: process}, nil
}

func downloadSharedNode() ([]byte, error) {
	client := http.Client{Timeout: 2 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("UNTRUSTED_NODE_DOWNLOAD_REDIRECT")
		}}
	response, err := client.Get(nodeOfficialURL)
	if err != nil {
		return nil, errors.New("OFFICIAL_NODE_DOWNLOAD_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("OFFICIAL_NODE_DOWNLOAD_UNAVAILABLE")
	}
	const max = 64 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil || len(data) > max || sha256Hash(data) != nodeArchiveHash {
		return nil, errors.New("OFFICIAL_NODE_ARCHIVE_MISMATCH")
	}
	return data, nil
}

type sharedNodeProcess struct{ Process }

func (p sharedNodeProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	if name == "node" {
		name = sharedPinnedNode
	}
	return p.Process.Run(name, args, input)
}
