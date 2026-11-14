package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type archiveFile func(string, os.FileMode, int64, io.Reader, string) error

var runnerNodeCommand = regexp.MustCompile(
	`^externals/node[0-9]+/bin/(corepack|npm|npx)$`)

func runnerLinkAllowed(name, target string) bool {
	if !runnerNodeCommand.MatchString(name) {
		return false
	}
	switch filepath.Base(name) {
	case "corepack":
		return target == "../lib/node_modules/corepack/dist/corepack.js"
	case "npm", "npx":
		return target == "../lib/node_modules/npm/bin/"+filepath.Base(name)+"-cli.js"
	default:
		return false
	}
}

func walkRunnerArchive(path string, zipped bool, handle archiveFile) error {
	seen := map[string]bool{}
	var total int64
	visit := func(name string, mode os.FileMode, size int64, reader io.Reader,
		link string) error {
		name = strings.TrimPrefix(name, "./")
		if mode.IsDir() && (name == "" || name == ".") {
			return nil
		}
		name = strings.TrimSuffix(name, "/")
		if !regexp.MustCompile(`^[A-Za-z0-9._/+@-]+$`).MatchString(name) ||
			strings.HasPrefix(name, "/") ||
			(!mode.IsRegular() && !mode.IsDir() &&
				!(mode&os.ModeSymlink != 0 && runnerLinkAllowed(name, link))) ||
			seen[name] || len(seen) >= 30000 || size < 0 || size > 512*1024*1024 {
			return errors.New("UNSAFE_RUNNER_ARCHIVE")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." {
				return errors.New("UNSAFE_RUNNER_ARCHIVE")
			}
		}
		seen[name] = true
		total += size
		if total > 2*1024*1024*1024 {
			return errors.New("RUNNER_ARCHIVE_SIZE_LIMIT")
		}
		if mode.IsDir() {
			return nil
		}
		return handle(name, mode, size, reader, link)
	}
	if zipped {
		archive, err := zip.OpenReader(path)
		if err != nil {
			return errors.New("INVALID_RUNNER_ARCHIVE")
		}
		defer archive.Close()
		for _, file := range archive.File {
			reader, err := file.Open()
			if err != nil {
				return errors.New("RUNNER_ARCHIVE_READ_FAILED")
			}
			visitErr := visit(file.Name, file.Mode(), int64(file.UncompressedSize64),
				reader, "")
			closeErr := reader.Close()
			if visitErr != nil || closeErr != nil {
				return errors.Join(visitErr, closeErr)
			}
		}
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("RUNNER_ARCHIVE_READ_FAILED")
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("INVALID_RUNNER_ARCHIVE")
	}
	defer compressed.Close()
	reader := tar.NewReader(io.LimitReader(compressed, 2*1024*1024*1024+1))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("INVALID_RUNNER_ARCHIVE")
		}
		mode := header.FileInfo().Mode()
		link := ""
		if mode&os.ModeSymlink != 0 {
			link = header.Linkname
		}
		if err := visit(header.Name, mode, header.Size, reader, link); err != nil {
			return err
		}
	}
}

func unpackRunner(archive, destination string, zipped bool) error {
	return walkRunnerArchive(archive, zipped,
		func(name string, mode os.FileMode, size int64, reader io.Reader, link string) error {
			path := filepath.Join(destination, filepath.FromSlash(name))
			if os.MkdirAll(filepath.Dir(path), 0700) != nil {
				return errors.New("RUNNER_EXTRACTION_FAILED")
			}
			if link != "" {
				if err := os.Symlink(filepath.FromSlash(link), path); err != nil {
					return errors.New("RUNNER_EXTRACTION_FAILED")
				}
				return nil
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY,
				0600|mode&0100)
			if err != nil {
				return errors.New("RUNNER_EXTRACTION_FAILED")
			}
			_, copyErr := io.CopyN(file, reader, size)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				return errors.New("RUNNER_EXTRACTION_FAILED")
			}
			return nil
		})
}

func verifyRunnerFiles(directory string, app RunnerApplication) error {
	return verifyRunnerPackage(directory, app, false)
}

func verifyRunnerPackage(directory string, app RunnerApplication, staged bool) error {
	if err := rejectRunnerOverrides(directory); err != nil {
		return err
	}
	archive := filepath.Join(directory, ".nibfin-runner-package")
	stat, err := os.Lstat(archive)
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 512*1024*1024 {
		return errors.New("UNTRUSTED_RUNNER_ARCHIVE")
	}
	file, err := os.Open(archive)
	if err != nil {
		return errors.New("RUNNER_ARCHIVE_UNAVAILABLE")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, io.LimitReader(file, 512*1024*1024+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != app.Hash {
		return errors.New("RUNNER_ARCHIVE_HASH_MISMATCH")
	}
	privatePaths := []string{directory, archive}
	privateDirectories := map[string]bool{directory: true}
	files := map[string]bool{archive: true, filepath.Join(directory, runnerRecordName): true}
	err = walkRunnerArchive(archive, strings.HasSuffix(app.Filename, ".zip"),
		func(name string, _ os.FileMode, size int64, reader io.Reader, link string) error {
			path := filepath.Join(directory, filepath.FromSlash(name))
			files[path] = true
			parent := filepath.Dir(path)
			for !privateDirectories[parent] {
				privateDirectories[parent] = true
				privatePaths = append(privatePaths, parent)
				parent = filepath.Dir(parent)
			}
			if link != "" {
				target, err := os.Readlink(path)
				if err != nil || target != filepath.FromSlash(link) {
					return errors.New("RUNNER_PACKAGE_CHANGED")
				}
				expected := filepath.Join(filepath.Dir(path), filepath.FromSlash(link))
				resolved, err := filepath.EvalSymlinks(path)
				if err != nil || resolved != expected {
					return errors.New("UNTRUSTED_RUNNER_FILE")
				}
				privatePaths = append(privatePaths, expected)
				return nil
			}
			privatePaths = append(privatePaths, path)
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || resolved != path {
				return errors.New("UNTRUSTED_RUNNER_FILE")
			}
			file, err := os.Open(path)
			if err != nil {
				return errors.New("RUNNER_PACKAGE_CHANGED")
			}
			actual, expected := sha256.New(), sha256.New()
			_, readErr := io.Copy(expected, reader)
			written, copyErr := io.Copy(actual, io.LimitReader(file, size+1))
			closeErr := file.Close()
			if readErr != nil || copyErr != nil || closeErr != nil || written != size ||
				hex.EncodeToString(actual.Sum(nil)) != hex.EncodeToString(expected.Sum(nil)) {
				return errors.New("RUNNER_PACKAGE_CHANGED")
			}
			return nil
		})
	if err != nil {
		return err
	}
	if staged {
		err := filepath.WalkDir(directory, func(path string, entry os.DirEntry,
			err error) error {
			if err != nil || !(files[path] || entry.IsDir() && privateDirectories[path]) {
				return errors.New("UNEXPECTED_STAGED_RUNNER_FILE")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return requirePrivateRunnerPaths(privatePaths)
}
