package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Record identities from the actual created files, before closing their handles.
// A later pathname snapshot must never adopt a foreign replacement as CLI data.
// Both bootstrap and operation-owned extraction use these same archive rules.
func extractCoreFiles(files map[string]coreArchiveFile,
	root *os.Root) (map[string]os.FileInfo, error) {
	entries := map[string]os.FileInfo{}
	info, err := root.Stat(".")
	if err != nil {
		return entries, errors.New("CORE_EXTRACTION_FAILED")
	}
	entries["."] = info
	for name, source := range files {
		path := filepath.FromSlash(name)
		dir := filepath.Dir(path)
		if err := createCoreDirectories(root, dir, entries); err != nil {
			return entries, err
		}
		parent, err := root.OpenRoot(dir)
		if err != nil {
			return entries, errors.New("CORE_EXTRACTION_FAILED")
		}
		info, err := parent.Stat(".")
		if err != nil || !os.SameFile(info, entries[dir]) {
			parent.Close()
			return entries, errors.New("CORE_EXTRACTION_DIRECTORY_CHANGED")
		}
		mode := os.FileMode(0600) | os.FileMode(source.mode&0100)
		file, err := parent.OpenFile(filepath.Base(path),
			os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		parent.Close()
		if err != nil {
			return entries, errors.New("CORE_EXTRACTION_FAILED")
		}
		_, writeErr := file.Write(source.data)
		info, statErr := file.Stat()
		closeErr := file.Close()
		if writeErr != nil || statErr != nil || closeErr != nil {
			return entries, errors.New("CORE_EXTRACTION_FAILED")
		}
		entries[path] = info
	}
	return entries, nil
}

func createCoreDirectories(root *os.Root, path string,
	entries map[string]os.FileInfo) error {
	if path == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(path, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if known, exists := entries[current]; exists {
			info, err := root.Lstat(current)
			if err != nil || !info.IsDir() || !os.SameFile(info, known) {
				return errors.New("CORE_EXTRACTION_DIRECTORY_CHANGED")
			}
			continue
		}
		// A first encounter must create the directory exclusively, even when
		// some foreign actor has added an archive-shaped path in the meantime.
		if err := root.Mkdir(current, 0700); err != nil {
			return errors.New("CORE_EXTRACTION_DIRECTORY_CHANGED")
		}
		info, err := root.Lstat(current)
		if err != nil || !info.IsDir() {
			return errors.New("CORE_EXTRACTION_DIRECTORY_CHANGED")
		}
		entries[current] = info
	}
	return nil
}
