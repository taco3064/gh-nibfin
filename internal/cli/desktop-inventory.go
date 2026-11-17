package cli

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

const desktopOutputLimit = 65536

var guestName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,63}$`)
var machineIdentity = regexp.MustCompile(`^[0-9a-f]{32}$`)

type desktopTarget struct {
	Provider    string                  `json:"provider"`
	Name        string                  `json:"name"`
	Running     bool                    `json:"running"`
	Version     int                     `json:"wsl_version,omitempty"`
	Identity    string                  `json:"identity,omitempty"`
	Linux       *linuxTargetObservation `json:"linux_target,omitempty"`
	GuestChecks []capability            `json:"guest_checks,omitempty"`
	Adopted     bool                    `json:"adopted"`
	Status      string                  `json:"status"`
	Gap         string                  `json:"gap,omitempty"`
}

func inventoryError() error { return errors.New("DESKTOP_INVENTORY_INVALID") }

func decodeWSL(data []byte) (string, error) {
	if len(data) > desktopOutputLimit || len(data)%2 != 0 {
		return "", inventoryError()
	}
	var order binary.ByteOrder = binary.LittleEndian
	if len(data) >= 2 {
		switch string(data[:2]) {
		case "\xff\xfe":
			data = data[2:]
		case "\xfe\xff":
			order, data = binary.BigEndian, data[2:]
		}
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = order.Uint16(data[2*i:])
	}
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u == 0 {
			return "", inventoryError()
		}
		if u >= 0xd800 && u <= 0xdbff {
			i++
			if i == len(units) || units[i] < 0xdc00 || units[i] > 0xdfff {
				return "", inventoryError()
			}
		} else if u >= 0xdc00 && u <= 0xdfff {
			return "", inventoryError()
		}
	}
	return string(utf16.Decode(units)), nil
}

func wslNames(data []byte) ([]string, error) {
	text, err := decodeWSL(data)
	if err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if !guestName.MatchString(name) || seen[strings.ToLower(name)] {
			return nil, inventoryError()
		}
		seen[strings.ToLower(name)] = true
		names = append(names, name)
	}
	if len(names) > 128 {
		return nil, inventoryError()
	}
	return names, nil
}

func parseWSL(quiet, verbose, running []byte) ([]desktopTarget, error) {
	names, err := wslNames(quiet)
	if err != nil {
		return nil, err
	}
	active, err := wslNames(running)
	if err != nil {
		return nil, err
	}
	text, err := decodeWSL(verbose)
	if err != nil {
		return nil, err
	}
	versions := map[string]int{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "*"))
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		last := fields[len(fields)-1]
		if last != "1" && last != "2" {
			continue
		}
		matches := 0
		for _, name := range names {
			if strings.HasPrefix(line, name) && len(line) > len(name) &&
				(line[len(name)] == ' ' || line[len(name)] == '\t') {
				matches++
				if versions[name] != 0 {
					return nil, inventoryError()
				}
				versions[name] = int(last[0] - '0')
			}
		}
		if matches != 1 {
			return nil, inventoryError()
		}
	}
	known := map[string]bool{}
	for _, name := range names {
		known[name] = true
	}
	for _, name := range active {
		if !known[name] {
			return nil, inventoryError()
		}
	}
	var targets []desktopTarget
	for _, name := range names {
		if versions[name] == 0 {
			return nil, inventoryError()
		}
		if strings.HasPrefix(strings.ToLower(name), "docker-desktop") {
			continue
		}
		t := desktopTarget{Provider: "wsl", Name: name, Version: versions[name],
			Status: "unverified", Gap: "STOPPED_GUEST_NOT_INSPECTED"}
		for _, a := range active {
			t.Running = t.Running || a == name
		}
		if t.Version == 1 {
			t.Status, t.Gap = "incompatible", "WSL_1_REQUIRES_WSL_2"
		}
		targets = append(targets, t)
	}
	return targets, nil
}

func parseMultipass(data []byte) ([]desktopTarget, error) {
	if len(data) > desktopOutputLimit {
		return nil, inventoryError()
	}
	var inventory struct {
		List []struct {
			Name    string `json:"name"`
			State   string `json:"state"`
			Release string `json:"release"`
		} `json:"list"`
	}
	if !boundedDesktopJSON(data) || json.Unmarshal(data, &inventory) != nil ||
		inventory.List == nil ||
		len(inventory.List) > 128 {
		return nil, inventoryError()
	}
	seen := map[string]bool{}
	var targets []desktopTarget
	for _, item := range inventory.List {
		if !guestName.MatchString(item.Name) || seen[item.Name] {
			return nil, inventoryError()
		}
		seen[item.Name] = true
		if !strings.HasPrefix(item.Release, "Ubuntu ") {
			continue
		}
		if item.State != "Running" && item.State != "Stopped" &&
			item.State != "Suspended" {
			return nil, inventoryError()
		}
		t := desktopTarget{Provider: "multipass", Name: item.Name,
			Running: item.State == "Running", Status: "unverified",
			Gap: "STOPPED_GUEST_NOT_INSPECTED"}
		if item.Release != "Ubuntu 24.04 LTS" && item.Release != "Ubuntu 24.04" {
			t.Status, t.Gap = "incompatible", "MANAGED_TARGET_REQUIRES_UBUNTU_24_04"
		}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}
