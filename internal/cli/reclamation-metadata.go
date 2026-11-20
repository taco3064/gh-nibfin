package cli

import (
	"os"
	"strings"
)

// The native adapter supplies registered identity and handle-based file metadata.
// Keeping these reads separate makes unknown metadata distinct from disabled state.
func inspectSparse(name string,
	backing func(string) (string, string, error),
	metadata func(string) (string, string, error),
	help func() ([]byte, error)) sparseObservation {
	o := sparseObservation{Setting: "unknown", Capability: "unavailable"}
	binding, path, err := backing(name)
	if err != nil || binding == "" || path == "" {
		if os.IsPermission(err) {
			o.Capability = "permission_denied"
		}
		return o
	}
	file, setting, err := metadata(path)
	if err != nil || file == "" || (setting != "enabled" && setting != "disabled") {
		if os.IsPermission(err) {
			o.Capability = "permission_denied"
		}
		return o
	}
	o.Binding, o.Setting = binding+"|"+file, setting
	output, err := help()
	if err != nil {
		if os.IsPermission(err) {
			o.Capability = "permission_denied"
		}
		return o
	}
	text := string(output)
	if strings.ContainsRune(text, 0) || strings.HasPrefix(text, "\xff\xfe") ||
		strings.HasPrefix(text, "\xfe\xff") {
		text, err = decodeWSL(output)
		if err != nil {
			return o
		}
	}
	o.Capability = "unsupported"
	if strings.Contains(text, "--manage") && strings.Contains(text, "--set-sparse") {
		o.Capability = "supported"
	}
	return o
}
