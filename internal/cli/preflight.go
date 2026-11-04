package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"runtime"
	"strings"
)

type capability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Code   string `json:"code"`
}

type preflightReport struct {
	HostOS    string       `json:"host_os"`
	HostArch  string       `json:"host_arch"`
	Core      SourcePin    `json:"core_candidate"`
	Checks    []capability `json:"checks"`
	Readiness string       `json:"protected_execution"`
}

func probe(name string, data []byte, err error) capability {
	if err != nil {
		// Errors from providers or tools are never safe output by default.
		code := "TOOL_INSPECTION_UNAVAILABLE"
		for _, safe := range []string{"TOOL_UNAVAILABLE", "UNTRUSTED_TOOL_PATH",
			"TEMP_DIRECTORY_UNAVAILABLE"} {
			if err.Error() == safe {
				code = safe
			}
		}
		return capability{name, "unverified", code}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return capability{name, "unverified", "EMPTY_TOOL_RESULT"}
	}
	return capability{name, "available", "VERSION_COMMAND_SUCCEEDED"}
}

func (a App) runPreflight(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("preflight", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Public OWNER/REPO")
	directory := flags.String("directory", "", "Optional managed Runner directory")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_PREFLIGHT_ARGUMENTS")
	}
	report := preflightReport{HostOS: runtime.GOOS, HostArch: runtime.GOARCH,
		Core: Pins().Core, Readiness: "unverified"}
	repo, user, err := repository(a.API, *name, true)
	permission := capability{"repository_admin", "unverified", "REPO_ADMIN_UNVERIFIED"}
	if err == nil {
		permission = capability{"repository_admin", "available", "CURRENT_ADMIN_VERIFIED"}
	}
	report.Checks = append(report.Checks, permission)
	for _, tool := range []string{"gh", "git", "node", "codex"} {
		data, err := a.Process.Run(tool, []string{"--version"}, nil)
		report.Checks = append(report.Checks, probe(tool, data, err))
	}
	data, loginErr := a.Process.Run("codex", []string{"login", "status"}, nil)
	login := capability{"codex_auth", "unverified", "CODEX_LOGIN_UNVERIFIED"}
	if loginErr == nil {
		switch strings.TrimSpace(string(data)) {
		case "chatgpt":
			login = capability{"codex_auth", "observed", "CHATGPT_LOGIN_PRESENT"}
		case "api_key":
			login = capability{"codex_auth", "observed", "API_KEY_LOGIN_PRESENT"}
		}
	}
	report.Checks = append(report.Checks, login,
		capability{"agent_execution", "unverified", "NO_AGENT_ROLE_EXECUTION_PERFORMED"})
	report.Checks = append(report.Checks, inspectDocker(a.Process)...)
	runner := capability{"official_runner", "unverified", "MANAGED_DIRECTORY_NOT_SUPPLIED"}
	if *directory != "" && permission.Status == "available" {
		runner.Code = "MANAGED_RUNNER_INSPECTION_FAILED"
		path, pathErr := runnerDirectory(*directory)
		if pathErr == nil {
			record, recordErr := readRunnerRecord(path, repo, user)
			if recordErr == nil {
				app, appErr := runnerApplication(a.API, repo.FullName)
				if appErr == nil && app == record.Application &&
					verifyRunnerFiles(path, app) == nil && a.Runner.Check(path) == nil {
					runner = capability{"official_runner", "available",
						"OFFICIAL_PACKAGE_AND_RUNTIME_VERIFIED"}
				}
			}
		}
	}
	report.Checks = append(report.Checks, runner,
		capability{"core_isolation", "unverified",
			"PINNED_CORE_HAS_NO_READ_ONLY_ISOLATION_PROOF_ENTRY"},
		capability{"protected_delivery", "unverified",
			"RUNNER_OIDC_AUTHORIZATION_AND_STAGE_EXECUTION_NOT_VERIFIED"})
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(report) != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	return &CommandError{Code: "PREFLIGHT_PROTECTED_EXECUTION_UNVERIFIED", Exit: 2}
}
