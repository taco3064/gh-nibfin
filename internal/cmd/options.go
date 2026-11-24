package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"

	"github.com/taco3064/gh-nibfin/internal/op"
)

// repoResolver turns the -R value into HOST/OWNER/REPO. An empty value means
// the current directory's repository. Tests replace it.
var repoResolver = resolveRepo

func resolveRepo(value string) (string, error) {
	if value != "" {
		r, err := repository.Parse(value)
		if err != nil {
			return "", op.Errorf(op.CodeInvalidRepo, "invalid repository %q: %w", value, err)
		}
		return r.Host + "/" + r.Owner + "/" + r.Name, nil
	}
	r, err := repository.Current()
	if err != nil {
		return "", op.Errorf(op.CodeRepoUnresolved, "cannot resolve the current repository: %w", err)
	}
	return r.Host + "/" + r.Owner + "/" + r.Name, nil
}

// addRepoFlag adds -R, --repo.
func addRepoFlag(c *cobra.Command, v *string) {
	c.Flags().StringVarP(v, "repo", "R", "", "target repository as [HOST/]OWNER/REPO (default: the current directory's repository)")
}

// addYesFlag adds --yes.
func addYesFlag(c *cobra.Command, v *bool) {
	c.Flags().BoolVar(v, "yes", false, "apply the plan without asking to confirm")
}

// addJSONFlag adds --json.
func addJSONFlag(c *cobra.Command, v *bool) {
	c.Flags().BoolVar(v, "json", false, "print the result as JSON")
}

// confirmer is the CLI Confirmer: --yes accepts, a terminal stdin is asked,
// anything else fails without waiting.
type confirmer struct {
	yes    bool
	stdin  io.Reader
	stderr io.Writer
}

func (c confirmer) Confirm(p op.Plan) (bool, error) {
	if c.yes {
		return true, nil
	}
	in, inOK := c.stdin.(*os.File)
	errOut, errOK := c.stderr.(*os.File)
	if !inOK || !errOK || !term.IsTerminal(in) {
		return false, op.Errorf(op.CodeConfirmationRequired, "%s needs confirmation; rerun with --yes", p.Operation)
	}
	fmt.Fprintf(errOut, "%s will:\n", p.Operation)
	for _, s := range p.Steps {
		line := "  " + strings.TrimSpace(s.Action+" "+s.Target)
		if s.Detail != "" {
			line += " (" + s.Detail + ")"
		}
		fmt.Fprintln(errOut, line)
	}
	return prompter.New(in, errOut, errOut).Confirm("Apply this plan?", false)
}
