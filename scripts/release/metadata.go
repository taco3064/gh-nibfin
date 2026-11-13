package main

import (
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type target struct{ OS, Arch string }

func targets() []target {
	return []target{{"linux", "amd64"}, {"linux", "arm64"},
		{"darwin", "amd64"}, {"darwin", "arm64"},
		{"windows", "amd64"}, {"windows", "arm64"}}
}

func (t target) filename() string {
	name := "gh-nibfin_" + t.OS + "_" + t.Arch
	if t.OS == "windows" {
		name += ".exe"
	}
	return name
}

type artifact struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type manifest struct {
	Version string     `json:"version"`
	Targets []artifact `json:"targets"`
}

func validateExecutable(path string, target target) error {
	valid := false
	switch target.OS {
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		machine := elf.EM_X86_64
		if target.Arch == "arm64" {
			machine = elf.EM_AARCH64
		}
		valid = file.Machine == machine && file.Type == elf.ET_EXEC
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		cpu := macho.CpuAmd64
		if target.Arch == "arm64" {
			cpu = macho.CpuArm64
		}
		valid = file.Cpu == cpu && file.Type == macho.TypeExec
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if target.Arch == "arm64" {
			machine = pe.IMAGE_FILE_MACHINE_ARM64
		}
		valid = file.Machine == machine && file.Characteristics&2 != 0
	}
	if !valid {
		return errors.New("unexpected executable format or architecture")
	}
	return nil
}

func describe(directory, name string) (artifact, error) {
	path := filepath.Join(directory, name)
	info, err := os.Lstat(path)
	if err != nil {
		return artifact{}, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return artifact{}, errors.New("expected nonempty regular artifact")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return artifact{}, err
	}
	digest := sha256.Sum256(data)
	return artifact{Filename: name, Size: info.Size(),
		SHA256: hex.EncodeToString(digest[:])}, nil
}

func writeMetadata(directory, version string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) != len(targets()) {
		return errors.New("unexpected artifact count")
	}
	result := manifest{Version: version}
	var sums strings.Builder
	addSum := func(item artifact) {
		fmt.Fprintf(&sums, "%s  %s\n", item.SHA256, item.Filename)
	}
	for _, target := range targets() {
		path := filepath.Join(directory, target.filename())
		item, err := describe(directory, target.filename())
		if err != nil {
			return err
		}
		if err := validateExecutable(path, target); err != nil {
			return err
		}
		if target.OS != "windows" {
			if err := os.Chmod(path, 0755); err != nil {
				return err
			}
		}
		item.OS, item.Arch = target.OS, target.Arch
		result.Targets = append(result.Targets, item)
		addSum(item)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return err
	}
	item, err := describe(directory, "manifest.json")
	if err != nil {
		return err
	}
	addSum(item)
	return os.WriteFile(filepath.Join(directory, "SHA256SUMS"),
		[]byte(sums.String()), 0644)
}
