package cli

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func wslUTF16(text string, big bool, bom bool) []byte {
	var order binary.ByteOrder = binary.LittleEndian
	var data []byte
	if big {
		order = binary.BigEndian
	}
	if bom {
		if big {
			data = []byte{0xfe, 0xff}
		} else {
			data = []byte{0xff, 0xfe}
		}
	}
	for _, unit := range utf16.Encode([]rune(text)) {
		var pair [2]byte
		order.PutUint16(pair[:], unit)
		data = append(data, pair[:]...)
	}
	return data
}

func TestDesktopWSLLocalizedInventory(t *testing.T) {
	for _, big := range []bool{false, true} {
		quiet := wslUTF16("Ubuntu Dev\r\nOld\r\ndocker-desktop\r\n", big, true)
		verbose := wslUTF16("名稱 狀態 版本\r\n"+
			"* Ubuntu Dev    執行中   2\r\n  Old 已停止 1\r\n"+
			"  docker-desktop 執行中 2\r\n", big, true)
		running := wslUTF16("Ubuntu Dev\r\ndocker-desktop\r\n", big, false)
		// Big endian output without a BOM is not a supported WSL encoding.
		if big {
			running = wslUTF16("Ubuntu Dev\ndocker-desktop\n", big, true)
		}
		targets, err := parseWSL(quiet, verbose, running)
		if err != nil || len(targets) != 2 {
			t.Fatalf("%+v %v", targets, err)
		}
		if !targets[0].Running || targets[0].Version != 2 ||
			targets[0].Name != "Ubuntu Dev" || targets[0].Status != "unverified" {
			t.Fatal(targets)
		}
		if targets[1].Status != "incompatible" || targets[1].Running {
			t.Fatal("WSL 1 not diagnosed", targets)
		}
	}
}

func TestDesktopWSLRejectsMalformedAndAmbiguousRecords(t *testing.T) {
	encode := func(s string) []byte { return wslUTF16(s, false, false) }
	for _, row := range []struct{ names, verbose, running string }{
		{"Ubuntu", "Ubuntu Running 3", ""},
		{"Ubuntu", "Ubuntu Running 2\nUbuntu Stopped 2", ""},
		{"Ubuntu\nUbuntu Dev", "Ubuntu Dev Running 2", ""},
		{"Ubuntu\nubuntu", "Ubuntu Running 2", ""},
		{"Ubuntu;touch bad", "Ubuntu;touch bad Running 2", ""},
		{"Ubuntu", "Ubuntu Running 2", "Unknown"},
		{"Ubuntu", "Unknown Running 2", ""},
	} {
		if _, err := parseWSL(encode(row.names), encode(row.verbose),
			encode(row.running)); err == nil {
			t.Fatalf("accepted %+v", row)
		}
	}
	for _, data := range [][]byte{{1}, {0, 0}, {0, 0xd8},
		make([]byte, desktopOutputLimit+2)} {
		if _, err := decodeWSL(data); err == nil {
			t.Fatal("bad UTF16 accepted")
		}
	}
}

func TestDesktopMultipassInventory(t *testing.T) {
	data := []byte(`{"list":[
  {"name":"ready","state":"Running","release":"Ubuntu 24.04 LTS"},
  {"name":"stopped","state":"Stopped","release":"Ubuntu 24.04 LTS"},
  {"name":"old","state":"Stopped","release":"Ubuntu 22.04 LTS"},
  {"name":"other","state":"Running","release":"Debian"}]}`)
	targets, err := parseMultipass(data)
	if err != nil || len(targets) != 3 {
		t.Fatalf("%+v %v", targets, err)
	}
	if targets[0].Status != "incompatible" || !targets[1].Running ||
		targets[2].Running || targets[2].Status != "unverified" {
		t.Fatal(targets)
	}
	for _, bad := range []string{`{}`, `{"list":null}`, `{"list":[],"list":[]}`,
		`{"list":[{"name":"bad;name","release":"Ubuntu 24.04"}]}`,
		`{"list":[{"name":"a","release":"Ubuntu 24.04","state":"Unknown"}]}`,
		`{"list":[]} {}`, strings.Repeat(" ", desktopOutputLimit+1)} {
		if _, err := parseMultipass([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad[:min(len(bad), 80)])
		}
	}
}

func TestDesktopSelectionRequiresUnambiguousChoice(t *testing.T) {
	targets := []desktopTarget{
		{Provider: "wsl", Name: "nibfin", Status: "eligible"},
		{Provider: "wsl", Name: "plain", Status: "eligible", Adopted: true},
	}
	index, err := chooseDesktopTarget(targets, "")
	if err != nil || index != 1 {
		t.Fatal(index, err)
	}
	targets[0].Adopted = true
	if _, err := chooseDesktopTarget(targets, ""); err == nil {
		t.Fatal("multiple adopted guests silently selected")
	}
	index, err = chooseDesktopTarget(targets, "wsl:nibfin")
	if err != nil || index != 0 {
		t.Fatal(index, err)
	}
	if _, err := chooseDesktopTarget(targets, "wsl:absent"); err == nil {
		t.Fatal("missing explicit target silently replaced")
	}
	targets[0].Status, targets[0].Gap = "incompatible", "WSL_1_REQUIRES_WSL_2"
	if _, err := chooseDesktopTarget(targets, "wsl:nibfin"); err == nil {
		t.Fatal("incompatible target selected")
	}
	targets = []desktopTarget{{Provider: "multipass", Name: "stopped",
		Status: "unverified", Running: false}}
	if index, err := chooseDesktopTarget(targets, ""); err != nil || index != 0 {
		t.Fatal("stopped target cannot be proposed with unverified state")
	}
}
