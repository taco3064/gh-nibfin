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
	name := flags.String("repo", "", "Public OWNER/REPO")
	version := flags.String("version", "", "Official stable vMAJOR.MINOR.PATCH")
	candidate := flags.String("candidate", "", "Exact official CLI candidate commit")
	apply := flags.Bool("apply", false, "Verify cache and open reviewed update PR")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *name == "" ||
		(*version == "") == (*candidate == "") ||
		*version != "" && !versionPattern.MatchString(*version) ||
		*candidate != "" && !commitPattern.MatchString(*candidate) {
		return errors.New("INVALID_UPGRADE_ARGUMENTS")
	}
	plan, err := planUpgrade(a.API, *name, upgradeSelection{*version, *candidate})
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"status": "preview", "plan": plan}); err != nil {
		return errors.New("OUTPUT_UNAVAILABLE")
	}
	if !*apply {
		return nil
	}
	result, err := a.applyUpgrade(plan)
	if err != nil {
		result.Code = err.Error()
	}
	if encoder.Encode(result) != nil {
		return &CommandError{Code: "OUTPUT_UNAVAILABLE_AFTER_UPGRADE", Exit: 2}
	}
	if err != nil || result.Status == "pending" {
		return &CommandError{Code: result.Code, Exit: 2}
	}
	return nil
}
