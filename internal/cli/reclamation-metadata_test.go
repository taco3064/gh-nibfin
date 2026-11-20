package cli

import (
	"errors"
	"os"
	"testing"
)

func TestSparseMetadataObservation(t *testing.T) {
	for _, scenario := range []string{"disabled", "enabled", "registration_unknown",
		"registration_denied", "file_unknown", "file_denied", "unsupported",
		"help_unknown", "utf16_help", "empty_identity"} {
		t.Run(scenario, func(t *testing.T) {
			fileReads, helpReads := 0, 0
			backing := func(name string) (string, string, error) {
				if name != "Ubuntu" {
					t.Fatal(name)
				}
				switch scenario {
				case "registration_unknown":
					return "", "", errors.New("ambiguous registration")
				case "registration_denied":
					return "", "", os.ErrPermission
				}
				return "registration", "private backing path", nil
			}
			metadata := func(path string) (string, string, error) {
				fileReads++
				if path != "private backing path" {
					t.Fatal(path)
				}
				switch scenario {
				case "file_unknown":
					return "", "unknown", errors.New("unreadable metadata")
				case "file_denied":
					return "", "unknown", os.ErrPermission
				case "empty_identity":
					return "", "disabled", nil
				case "enabled":
					return "volume:file", "enabled", nil
				}
				return "volume:file", "disabled", nil
			}
			help := func() ([]byte, error) {
				helpReads++
				switch scenario {
				case "help_unknown":
					return nil, errors.New("help unavailable")
				case "unsupported":
					return []byte("--list --manage"), nil
				case "utf16_help":
					return wslUTF16("--manage --set-sparse", true, true), nil
				}
				return []byte("--manage --set-sparse"), nil
			}
			want := sparseObservation{"registration|volume:file", "disabled", "supported"}
			switch scenario {
			case "enabled":
				want.Setting = "enabled"
			case "registration_unknown", "file_unknown", "empty_identity":
				want = sparseObservation{Setting: "unknown", Capability: "unavailable"}
			case "registration_denied", "file_denied":
				want = sparseObservation{Setting: "unknown", Capability: "permission_denied"}
			case "unsupported":
				want.Capability = "unsupported"
			case "help_unknown":
				want.Capability = "unavailable"
			}
			if got := inspectSparse("Ubuntu", backing, metadata, help); got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
			if want.Setting == "unknown" && helpReads != 0 {
				t.Fatal("queried capability without metadata")
			}
			if scenario == "registration_unknown" && fileReads != 0 {
				t.Fatal("inspected a file without registered identity")
			}
		})
	}
}
