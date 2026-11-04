package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Process interface {
	Run(string, []string, []byte) ([]byte, error)
}

type NativeProcess struct{}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2*1024*1024 {
		return 0, errors.New("PROCESS_OUTPUT_LIMIT")
	}
	return b.Buffer.Write(p)
}

func toolPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.New("TOOL_UNAVAILABLE")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil || !filepath.IsAbs(path) {
		return "", errors.New("UNTRUSTED_TOOL_PATH")
	}
	if hasGitAncestor(filepath.Dir(path)) {
		return "", errors.New("UNTRUSTED_TOOL_PATH")
	}
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", errors.New("TEMP_DIRECTORY_UNAVAILABLE")
	}
	rel, err := filepath.Rel(temporary, path)
	if err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("UNTRUSTED_TOOL_PATH")
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.Mode().IsRegular() ||
		runtime.GOOS != "windows" && stat.Mode().Perm()&0002 != 0 {
		return "", errors.New("UNTRUSTED_TOOL_PATH")
	}
	if runtime.GOOS != "windows" {
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			stat, err := os.Stat(parent)
			if err != nil || stat.Mode().Perm()&0002 != 0 {
				return "", errors.New("UNTRUSTED_TOOL_PATH")
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
	}
	return path, nil
}

func (NativeProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	path, err := toolPath(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if name == "node" {
		for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP"} {
			if value, ok := os.LookupEnv(key); ok {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
	}
	cmd.Stdin = bytes.NewReader(input)
	var out, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	if err != nil {
		// Only the official gh HTTP diagnostic is classified; never disclose it.
		if name == "gh" && strings.Contains(stderr.String(), "(HTTP 404)") {
			return nil, errors.New("NOT_FOUND")
		}
		if name == "gh" && strings.Contains(stderr.String(), "(HTTP 403)") {
			return nil, errors.New("PERMISSION_DENIED")
		}
		return nil, errors.New("PROCESS_RESULT_UNAVAILABLE")
	}
	return out.Bytes(), nil
}
