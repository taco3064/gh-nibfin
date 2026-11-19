package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMaintenanceBridgeDelegatesInterruptedManagerRepairToRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires isolated eligible root-owned fixture ancestors")
	}
	for _, interrupted := range []bool{false, true} {
		t.Run(strconv.FormatBool(interrupted), func(t *testing.T) {
			dir := t.TempDir()
			marker, log := filepath.Join(dir, "incomplete"), filepath.Join(dir, "calls")
			if interrupted {
				if err := os.WriteFile(marker, []byte("C-tree/B-pin/no-runtime"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			receipt, _ := json.Marshal(maintenanceReceipt{Schema: 1, Status: "installed",
				Capability: maintenanceCapability, Source: SourcePin{Repository: officialCore,
					Commit: strings.Repeat("b", 40), ManifestHash: strings.Repeat("c", 64)}})
			program := "#!/usr/bin/python3 -I\nimport json,pathlib,sys\n" +
				"marker=pathlib.Path(" + strconv.Quote(marker) + ")\n" +
				"log=pathlib.Path(" + strconv.Quote(log) + ")\n" +
				"kind=json.load(sys.stdin)['kind']\n" +
				"with log.open('a') as out: out.write(kind+'\\n')\n" +
				"if kind=='inspect' and marker.exists():\n" +
				"    print('{\"status\":\"pending\",\"code\":\"MANAGER_REPAIR\"}')\n" +
				"    raise SystemExit(2)\n" +
				"if kind=='prepare': marker.unlink()\n" +
				"print(" + strconv.Quote(string(receipt)) + ")\n"
			path := filepath.Join(dir, "fixed-bridge.py")
			if err := os.WriteFile(path, []byte(program), 0700); err != nil {
				t.Fatal(err)
			}
			process := coreResultProcess{ctx: context.Background(),
				resolve: func(name string) (string, error) {
					if name != maintenanceBridge {
						t.Fatal("client selected a foreign privileged entry", name)
					}
					return path, nil
				}}
			client := maintenanceBridgeClient{process,
				func() error { return trustedMaintenanceExecutable(path) }}
			if err := client.prepare(); err != nil {
				t.Fatal("client blocked root's normal manager recovery", err)
			}
			calls, err := os.ReadFile(log)
			want := "inspect\n"
			if interrupted {
				want += "prepare\n"
			}
			if err != nil || string(calls) != want {
				t.Fatal("healthy manager mutated or repair was unreachable", string(calls), err)
			}
			if err := client.prepare(); err != nil {
				t.Fatal("confirmed repair was not reusable", err)
			}
		})
	}
}

func TestMaintenanceBridgeRejectsUntrustedExecutablePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.py")
	if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0600, 0777} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := trustedMaintenanceExecutable(path); err == nil {
			t.Fatal("non-executable or writable bridge trusted", mode)
		}
	}
	link := filepath.Join(dir, "foreign.py")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := trustedMaintenanceExecutable(link); err == nil {
		t.Fatal("foreign symlink became a privileged entry")
	}
}
