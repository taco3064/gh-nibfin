package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Windows packaged-app virtualization can give Go and Node different canonical
// spellings of the same directory. Use Node's view only after file-ID equality;
// Core keeps its original strict path verification, including child paths.
func coreVisibleDirectory(process Process, directory string) (string, error) {
	const script = `import { realpath } from 'node:fs/promises';
process.stdout.write(JSON.stringify(await realpath(process.argv[1])));`
	data, err := process.Run("node", []string{"--input-type=module", "-e",
		script, directory}, nil)
	var canonical string
	if err != nil || json.Unmarshal(data, &canonical) != nil ||
		!filepath.IsAbs(canonical) || filepath.Clean(canonical) != canonical {
		return "", errors.New("CORE_CACHE_CANONICAL_PATH_UNAVAILABLE")
	}
	original, originalErr := os.Stat(directory)
	resolved, resolvedErr := os.Stat(canonical)
	if originalErr != nil || resolvedErr != nil || !original.IsDir() ||
		!resolved.IsDir() || !os.SameFile(original, resolved) {
		return "", errors.New("CORE_CACHE_DIRECTORY_IDENTITY_MISMATCH")
	}
	return canonical, nil
}
