package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type changeEvent struct {
	Before      string
	PullRequest struct{ Base struct{ SHA string } } `json:"pull_request"`
}

type testPackage struct {
	Dir, ImportPath                 string
	Deps, TestImports, XTestImports []string
	TestGoFiles, XTestGoFiles       []string
}

func output(program string, args ...string) ([]byte, error) {
	command := exec.Command(program, args...)
	command.Stderr = os.Stderr
	return command.Output()
}

func changes() ([]string, error) {
	args := []string{"diff", "--cached"}
	if path := os.Getenv("GITHUB_EVENT_PATH"); path != "" {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var event changeEvent
		if err := json.Unmarshal(bytes, &event); err != nil {
			return nil, err
		}
		base := event.PullRequest.Base.SHA
		if base == "" {
			base = event.Before
		}
		if base == strings.Repeat("0", 40) {
			result, err := output("git", "merge-base", "origin/main", "HEAD")
			if err != nil {
				return nil, err
			}
			base = strings.TrimSpace(string(result))
		}
		if len(base) != 40 || strings.Trim(base, "0123456789abcdef") != "" {
			return nil, fmt.Errorf("CI diff base unavailable")
		}
		args = []string{"diff", base, "HEAD"}
	}
	result, err := output("git", append(args, "--name-only", "--no-renames", "-z", "--")...)
	return strings.FieldsFunc(string(result), func(r rune) bool { return r == 0 }), err
}

func affectedPackages(paths []string) ([]string, bool, error) {
	if len(paths) == 0 {
		return nil, false, nil
	}
	result, err := output("go", "list", "-json", "./...")
	if err != nil {
		return nil, false, err
	}
	root, err := os.Getwd()
	if err != nil {
		return nil, false, err
	}
	packages := map[string]testPackage{}
	directories := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(result))
	for {
		var item testPackage
		err := decoder.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, err
		}
		dir, err := filepath.Rel(root, item.Dir)
		if err != nil {
			return nil, false, err
		}
		packages[item.ImportPath] = item
		directories[filepath.ToSlash(dir)] = item.ImportPath
	}
	direct := map[string]bool{}
	all := false
	for _, path := range paths {
		if path == "go.mod" || path == "go.sum" || path == ".github/workflows/ci.yml" ||
			strings.HasPrefix(path, "scripts/staged/") {
			all = true
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		if dir == "." && !strings.HasSuffix(path, ".go") {
			continue
		}
		for dir != "." && directories[dir] == "" {
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
		if dir != "." || strings.HasSuffix(path, ".go") {
			direct[directories[dir]] = true
		}
	}
	affected := func(item testPackage) bool {
		return all || direct[item.ImportPath] || slices.ContainsFunc(item.Deps,
			func(dependency string) bool { return direct[dependency] })
	}
	var selected []string
	cache := false
	for name, item := range packages {
		if len(item.TestGoFiles)+len(item.XTestGoFiles) == 0 {
			continue
		}
		testImports := append(slices.Clone(item.TestImports), item.XTestImports...)
		if affected(item) || slices.ContainsFunc(testImports, func(dependency string) bool {
			return affected(packages[dependency])
		}) {
			selected = append(selected, name)
			cache = cache || strings.HasSuffix(name, "/internal/cli")
		}
	}
	slices.Sort(selected)
	return selected, cache, nil
}

func run() error {
	var paths []string
	var err error
	if index := slices.Index(os.Args, "--paths"); index >= 0 {
		paths = os.Args[index+1:]
	} else {
		paths, err = changes()
	}
	if err != nil {
		return err
	}
	packages, cache, err := affectedPackages(paths)
	if err != nil {
		return err
	}
	if slices.Contains(os.Args[1:], "--cache") ||
		slices.Contains(os.Args[1:], "--browser") {
		line := fmt.Sprintf("cache=%t\n", cache)
		if slices.Contains(os.Args[1:], "--browser") {
			line = fmt.Sprintf("browser=%t\n", browserAffected(paths))
		}
		if path := os.Getenv("GITHUB_OUTPUT"); path != "" {
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = file.WriteString(line)
			return err
		}
		fmt.Print(line)
		return nil
	}
	fmt.Printf("Selected %d packages for %d changed paths.\n", len(packages), len(paths))
	if len(packages) != 0 {
		args := append([]string{"test", "-timeout=4m"}, packages...)
		command := exec.Command("go", args...)
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Run(); err != nil {
			return err
		}
	}
	if browserAffected(paths) {
		return runBrowser()
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
