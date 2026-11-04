package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func unpackCore(archive []byte, destination string) error {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return errors.New("INVALID_CORE_ARCHIVE")
	}
	defer compressed.Close()
	reader := tar.NewReader(io.LimitReader(compressed, 40*1024*1024+1))
	paths := map[string]bool{}
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("INVALID_CORE_ARCHIVE")
		}
		name := header.Name
		if !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(name) ||
			strings.HasPrefix(name, "/") || header.Typeflag != tar.TypeReg ||
			(header.Mode != 0644 && header.Mode != 0755) ||
			paths[name] || len(paths) >= 1001 || header.Size < 0 ||
			header.Size > 32*1024*1024 {
			return errors.New("UNSAFE_CORE_ARCHIVE")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." || part == ".git" {
				return errors.New("UNSAFE_CORE_ARCHIVE")
			}
		}
		total += header.Size
		if total > 32*1024*1024 {
			return errors.New("CORE_ARCHIVE_SIZE_LIMIT")
		}
		paths[name] = true
		path := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return errors.New("CORE_EXTRACTION_FAILED")
		}
		mode := os.FileMode(0600) | os.FileMode(header.Mode&0100)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return errors.New("CORE_EXTRACTION_FAILED")
		}
		_, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return errors.New("CORE_EXTRACTION_FAILED")
		}
	}
	if !paths["bin/nibfin-core.mjs"] || !paths[".node-version"] {
		return errors.New("INCOMPLETE_CORE_ARCHIVE")
	}
	return nil
}
