//go:build !linux

package cli

import "os"

func privateCacheInfo(os.FileInfo, bool) bool { return false }
func nativeCacheUse(string) string            { return "unknown_observation" }
func retainedCacheSource(string) string       { return "unknown_observation" }
