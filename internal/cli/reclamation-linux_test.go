package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestLinuxReclamationWithoutInterop(t *testing.T) {
	files := map[string]string{
		"/proc/sys/kernel/osrelease":                    "6.6-microsoft-standard-WSL2",
		"/proc/version":                                 "Linux Microsoft",
		"/proc/self/mountinfo":                          "1 0 8:1 / / rw - ext4 /dev/sda1 rw\n",
		"/sys/dev/block/8:1/partition":                  "1",
		"/sys/dev/block/8:1/../queue/discard_max_bytes": "4096",
	}
	read := func(path string) ([]byte, error) {
		if value, ok := files[path]; ok {
			return []byte(value), nil
		}
		return nil, errors.New("unavailable")
	}
	o := observeLinuxReclamation(read, func(name string) bool { return name == "fstrim" })
	if o.WSL != "observed" || o.Discard != "supported" ||
		o.RootFilesystem != "ext4" || o.TrimTool != "available" ||
		!strings.Contains(o.Guidance, "--target wsl:NAME") {
		t.Fatalf("%+v", o)
	}
	files["/sys/dev/block/8:1/../queue/discard_max_bytes"] = "0"
	o = observeLinuxReclamation(read, func(string) bool { return false })
	if o.Discard != "unsupported" {
		t.Fatalf("%+v", o)
	}
	delete(files, "/sys/dev/block/8:1/../queue/discard_max_bytes")
	files["/proc/sys/kernel/osrelease"] = "6.6-linux"
	files["/proc/version"] = "Linux"
	o = observeLinuxReclamation(read, func(string) bool { return false })
	if o.WSL != "not_observed" || o.Discard != "unknown" {
		t.Fatalf("%+v", o)
	}
	o.Guidance, o.RootFilesystem = "secret", "secret"
	clean := sanitizedReclamation(o)
	if clean.Guidance != "" || clean.RootFilesystem != "unknown" {
		t.Fatal(clean)
	}
}
