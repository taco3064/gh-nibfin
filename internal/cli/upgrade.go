package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
)

func (a App) runUpgrade(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Initialized public OWNER/REPO")
	version := flags.String("version", "latest", "Official vMAJOR.MINOR.PATCH or latest")
	apply := flags.Bool("apply", false, "Propose an upgrade through a protected PR")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *name == "" ||
		*version != "latest" && !versionPattern.MatchString(*version) {
		return errors.New("INVALID_UPGRADE_ARGUMENTS")
	}
	plan, err := planUpgrade(a.API, *name, *version)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	status := "preview"
	if len(plan.Changes) == 0 {
		status = "no_change"
	}
	if err := encoder.Encode(map[string]any{"status": status, "plan": plan}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	result, err := applyUpgrade(a.API, plan, *version)
	if err != nil {
		result.Code = err.Error()
		_ = encoder.Encode(result)
		return &CommandError{Code: "UPGRADE_PENDING_INSPECT_READ_BACK_BEFORE_RETRY", Exit: 2}
	}
	if err := encoder.Encode(result); err != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_UPGRADE", Exit: 2}
	}
	return nil
}
