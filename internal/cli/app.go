package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

type App struct {
	API     API
	Process Process
}

type CommandError struct {
	Code string
	Exit int
}

func (e *CommandError) Error() string { return e.Code }

func (a App) Run(args []string, out io.Writer) error {
	if len(args) == 0 || len(args) == 1 &&
		(args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "gh nibfin: manual Bootstrap candidate\n"+
			"Usage: gh nibfin init --repo OWNER/REPO [--apply]\n"+
			"Default: preview only. --apply opens an installation PR; never merges it.\n"+
			"Runner and environment commands are not implemented yet.")
		return err
	}
	if args[0] != "init" {
		return errors.New("UNKNOWN_COMMAND_USE_HELP")
	}
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Public OWNER/REPO")
	apply := flags.Bool("apply", false, "Create reviewed installation PR and labels")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_INIT_ARGUMENTS")
	}
	plan, err := planInit(a.API, *name)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"status": "preview", "plan": plan,
		"protected_execution": "unverified"}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	cache, err := installCoreCache(a.API, a.Process)
	if err != nil {
		return err
	}
	if err := encoder.Encode(map[string]any{"status": "candidate_source_verified",
		"core_cache": cache, "host_adoption": "unverified"}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	result, err := applyInit(a.API, plan)
	if err != nil {
		result.Code = err.Error()
		_ = encoder.Encode(result)
		return &CommandError{Code: "INSTALLATION_INCOMPLETE_READ_BACK_REQUIRED", Exit: 2}
	}
	if err := encoder.Encode(result); err != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_INSTALLATION", Exit: 2}
	}
	if result.Status == "pending" {
		return &CommandError{Code: result.Code, Exit: 2}
	}
	return nil
}
