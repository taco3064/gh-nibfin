package cmd

import (
	"bufio"
	"context"
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

// terminal returns the TTY to attach a login to, or nil when stdin or stdout
// is not a terminal.
func terminal(stdin io.Reader, stdout, stderr io.Writer) *op.Terminal {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK || !term.IsTerminal(in) || !term.IsTerminal(out) {
		return nil
	}
	errOut, ok := stderr.(*os.File)
	if !ok {
		errOut = out
	}
	return &op.Terminal{In: in, Out: out, Err: errOut}
}

// interactor is the CLI side of a structured login: steps go to stderr and
// a pasted code is read from stdin.
type interactor struct {
	in  *bufio.Reader
	out io.Writer
}

func (i *interactor) Step(s op.LoginStep) {
	switch s.Kind {
	case op.StepOpenURL:
		fmt.Fprintf(i.out, "open %s\n", s.URL)
	case op.StepDeviceCode:
		fmt.Fprintf(i.out, "enter code %s\n", s.Code)
	case op.StepCodeRequested:
		fmt.Fprint(i.out, "paste the code shown in your browser: ")
	}
}

func (i *interactor) Code(context.Context) (string, error) {
	line, err := i.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", op.Errorf(op.CodeCancelled, "login cancelled: no code was entered")
	}
	return strings.TrimSpace(line), nil
}
