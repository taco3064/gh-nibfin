package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

type App struct {
	Dashboard *DashboardRuntime
	API       API
	Process   Process
	Runner    RunnerHost
	Download  RunnerDownload
	Clock     func() time.Time
}

type CommandError struct {
	Code string
	Exit int
}

func (e *CommandError) Error() string { return e.Code }

func (a App) Run(args []string, out io.Writer) error {
	return a.RunContext(context.Background(), args, out)
}

func (a App) RunContext(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) == 1 &&
		(args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, "gh nibfin: manual Bootstrap candidate\n"+
			"Usage: gh nibfin init --repo OWNER/REPO [--apply]\n"+
			"Default: preview only. Plain init --apply opens a PR; never merges it.\n"+
			"Optional Runner setup: --runner-directory PATH --runner-name NAME.\n"+
			"Member handoff: init --quick request/issue/register/confirm/reconcile\n"+
			"Recovery: init --quick inspect/cancel; see README for the full sequence.\n"+
			"Use --handoff-directory PATH with each step and --apply to execute.\n"+
			"Management issues/confirms; members use their own GitHub login.\n"+
			"Runner: create/register/list/unregister/remove --repo OWNER/REPO\n"+
			"Mutations require --directory ABSOLUTE_PATH --apply; create also --name.\n"+
			dashboardHelp+"\n"+
			"Inspect: gh nibfin preflight --repo OWNER/REPO [--directory RUNNER_PATH]")
		return err
	}
	if args[0] == "dashboard" {
		return a.runDashboard(ctx, args[1:], out)
	}
	if args[0] == "runner" {
		return a.runRunner(args[1:], out)
	}
	if args[0] == "preflight" {
		return a.runPreflight(args[1:], out)
	}
	if args[0] != "init" {
		return errors.New("UNKNOWN_COMMAND_USE_HELP")
	}
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Public OWNER/REPO")
	apply := flags.Bool("apply", false, "Create reviewed installation PR and labels")
	runnerPath := flags.String("runner-directory", "", "Private Runner directory")
	runnerName := flags.String("runner-name", "", "Name of the repository Runner")
	quick := addQuickFlags(flags)
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *name == "" {
		return errors.New("INVALID_INIT_ARGUMENTS")
	}
	if quick.used() {
		return a.runQuick(*name, *runnerPath, *runnerName, *apply, quick, out)
	}
	plan, err := planInit(a.API, *name)
	if err != nil {
		return err
	}
	var runner *initRunner
	if *runnerPath != "" || *runnerName != "" {
		runner, err = a.planInitRunner(plan, *runnerPath, *runnerName)
		if err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	preview := map[string]any{"status": "preview", "plan": plan,
		"protected_execution": "unverified"}
	if runner != nil {
		preview["runner_setup"] = runner
	}
	if err := encoder.Encode(preview); err != nil {
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
	if runner != nil {
		return a.applyInitRunner(plan, *runner, out)
	}
	return nil
}
