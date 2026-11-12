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

type coreArchiveFile struct {
	data []byte
	mode int64
}

// Parse once for both read-only source verification and cache extraction.
func readCoreArchive(archive []byte) (map[string]coreArchiveFile, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, errors.New("INVALID_CORE_ARCHIVE")
	}
	defer compressed.Close()
	reader := tar.NewReader(io.LimitReader(compressed, 40*1024*1024+1))
	files := map[string]coreArchiveFile{}
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("INVALID_CORE_ARCHIVE")
		}
		name := header.Name
		_, duplicate := files[name]
		if !regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(name) ||
			strings.HasPrefix(name, "/") || header.Typeflag != tar.TypeReg ||
			(header.Mode != 0644 && header.Mode != 0755) || duplicate ||
			len(files) >= 1001 || header.Size < 0 || header.Size > 32*1024*1024 {
			return nil, errors.New("UNSAFE_CORE_ARCHIVE")
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || part == "." || part == ".." || part == ".git" {
				return nil, errors.New("UNSAFE_CORE_ARCHIVE")
			}
		}
		total += header.Size
		if total > 32*1024*1024 {
			return nil, errors.New("CORE_ARCHIVE_SIZE_LIMIT")
		}
		data, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(data)) != header.Size {
			return nil, errors.New("INVALID_CORE_ARCHIVE")
		}
		files[name] = coreArchiveFile{data: data, mode: header.Mode}
	}
	if _, ok := files["bin/nibfin-core.mjs"]; !ok {
		return nil, errors.New("INCOMPLETE_CORE_ARCHIVE")
	}
	if _, ok := files[".node-version"]; !ok {
		return nil, errors.New("INCOMPLETE_CORE_ARCHIVE")
	}
	return files, nil
}

func unpackCore(archive []byte, destination string) error {
	files, err := readCoreArchive(archive)
	if err != nil {
		return err
	}
	for name, source := range files {
		path := filepath.Join(destination, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return errors.New("CORE_EXTRACTION_FAILED")
		}
		mode := os.FileMode(0600) | os.FileMode(source.mode&0100)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return errors.New("CORE_EXTRACTION_FAILED")
		}
		_, writeErr := file.Write(source.data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return errors.New("CORE_EXTRACTION_FAILED")
		}
	}
	return nil
}
