// Command release produces a complete local release set without publishing it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.` +
	`(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
	`(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?` +
	`(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

func normalizeVersion(value string) (string, error) {
	value = strings.TrimPrefix(value, "v")
	if !semver.MatchString(value) {
		return "", errors.New("explicit semantic version required")
	}
	pre := strings.SplitN(value, "+", 2)[0]
	if i := strings.IndexByte(pre, '-'); i >= 0 {
		for _, part := range strings.Split(pre[i+1:], ".") {
			if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
				return "", errors.New("numeric prerelease identifiers cannot have leading zeros")
			}
		}
	}
	return value, nil
}

type buildFunc func(root, destination, version string, target target) error

func build(root, destination, version string, target target) error {
	linker := "-s -w -X github.com/taco3064/gh-nibfin/internal/cli.Version=" + version
	command := exec.Command("go", "build", "-buildvcs=false", "-trimpath",
		"-ldflags", linker, "-o", destination, ".")
	command.Dir = root
	// Do not inherit cross-build or compiler overrides from the caller.
	for _, item := range os.Environ() {
		key := strings.SplitN(item, "=", 2)[0]
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOENV", "GOWORK":
			continue
		}
		command.Env = append(command.Env, item)
	}
	command.Env = append(command.Env, "GOOS="+target.OS, "GOARCH="+target.Arch,
		"CGO_ENABLED=0", "GOFLAGS=", "GOENV=off", "GOWORK=off")
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

func produce(root, version string, builder buildFunc) error {
	version, err := normalizeVersion(version)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(module), "module github.com/taco3064/gh-nibfin\n") {
		return errors.New("run from the gh-nibfin repository root")
	}
	destination := filepath.Join(root, "dist")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return errors.New("dist must not exist; move the previous set aside explicitly")
	}
	stage, err := os.MkdirTemp(root, ".release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, target := range targets() {
		path := filepath.Join(stage, target.filename())
		if err := builder(root, path, version, target); err != nil {
			return fmt.Errorf("build %s/%s: %w", target.OS, target.Arch, err)
		}
	}
	if err := writeMetadata(stage, version); err != nil {
		return err
	}
	// Recheck before installation; never merge a partial set with an existing one.
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return errors.New("dist appeared during build")
	}
	return os.Rename(stage, destination)
}

func run(args []string) error {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	version := flags.String("version", "", "Explicit semantic version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected release arguments")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	return produce(root, *version, build)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
