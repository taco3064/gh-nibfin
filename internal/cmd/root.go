// Package cmd is the gh nibfin command tree. Each handler maps its arguments
// onto one entry point of the shared operation layer: internal/op, or a
// package of that layer such as internal/upgrade.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/taco3064/gh-nibfin/internal/initrepo"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/status"
	"github.com/taco3064/gh-nibfin/internal/upgrade"
)

// ops holds the operation entry points the handlers call. Tests replace it.
var ops = struct {
	RunnerAdd    func(op.RunnerAddParams) op.Operation
	RunnerRemove func(op.RunnerRemoveParams) op.Operation
	Login        func(op.LoginParams) op.Operation
	Init         func(op.RepoParams) op.Operation
	Upgrade      func(op.RepoParams) op.Operation
	Status       func(op.RepoParams) op.Operation
	Pause        func(op.IssuesParams) op.Operation
	Resume       func(op.IssuesParams) op.Operation
}{op.RunnerAdd, op.RunnerRemove, op.Login, initrepo.New, upgrade.New, status.New, op.Pause, op.Resume}

var providers = []string{"claude", "codex"}

// Execute runs gh nibfin with args and returns the process exit code. An
// error goes to stderr as "<code>: <message>"; with --json it is also written
// to stdout as {"code","message"}.
func Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var jsonOut bool
	root := newRoot(stdin, stdout, stderr, &jsonOut)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if _, err := root.ExecuteC(); err != nil {
		// Every error from a handler carries a code; one without a code comes
		// from cobra's argument and flag parsing.
		if op.Code(err) == "" {
			err = op.Wrap(op.CodeInvalidArgument, err)
		}
		e := op.AsError(err)
		fmt.Fprintf(stderr, "%s: %s\n", e.Code, e.Error())
		if jsonOut {
			writeJSON(stdout, e)
		}
		return 1
	}
	return 0
}

func newRoot(stdin io.Reader, stdout, stderr io.Writer, jsonOut *bool) *cobra.Command {
	root := &cobra.Command{
		Use:           "nibfin",
		Annotations:   map[string]string{cobra.CommandDisplayNameAnnotation: "gh nibfin"},
		Short:         "Run and manage Nibfin from the GitHub CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return op.Wrap(op.CodeInvalidArgument, err)
	})
	root.CompletionOptions.DisableDefaultCmd = true

	// run executes a changing operation through op.Run with the CLI Confirmer.
	run := func(c *cobra.Command, o op.Operation, yes bool) error {
		_, r, err := op.Run(c.Context(), o, confirmer{yes: yes, stdin: stdin, stderr: stderr})
		if err != nil {
			return err
		}
		writeResult(stdout, r, false)
		return nil
	}

	runner := &cobra.Command{
		Use:   "runner",
		Short: "Add or remove local Runners",
		// Without RunE cobra shows help and exits 0 for an unknown
		// subcommand, so both a missing and an unknown one fail here.
		Args: func(c *cobra.Command, args []string) error {
			if len(args) > 0 {
				return op.Errorf(op.CodeInvalidArgument, "unknown command %q for %q", args[0], c.CommandPath())
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			return op.Errorf(op.CodeInvalidArgument, "%q requires a subcommand: add or remove", c.CommandPath())
		},
	}

	var addCount int
	var addYes bool
	runnerAdd := &cobra.Command{
		Use:   "add <claude|codex>",
		Short: "Add Runners for a provider on this host",
		Long: "Add Runners for a provider on this host (the Linux host, or the podman machine VM).\n\n" +
			"The first Runner on the host registers to the current directory's repository; later\n" +
			"Runners reuse the repository the existing Runners use. Each Runner carries the labels\n" +
			"nibfin and nibfin-<user id>-<provider>, and runs as a systemd user service.",
		Args:      providerArgs(1, 1),
		ValidArgs: providers,
		RunE: func(c *cobra.Command, args []string) error {
			if err := checkCount(addCount); err != nil {
				return err
			}
			p := op.RunnerAddParams{Provider: args[0], Count: addCount}
			p.Terminal, p.Interact = loginIO(stdin, stdout, stderr)
			return run(c, ops.RunnerAdd(p), addYes)
		},
	}
	runnerAdd.Flags().IntVar(&addCount, "count", 2, "number of Runners")
	addYesFlag(runnerAdd, &addYes)

	var removeCount int
	var removeYes, deleteLogin bool
	runnerRemove := &cobra.Command{
		Use:   "remove <claude|codex>",
		Short: "Remove Runners for a provider on this host",
		Long: "Remove Runners for a provider on this host (the Linux host, or the podman machine VM).\n\n" +
			"Without --count every Runner of the provider is removed. Idle Runners go first. A busy\n" +
			"Runner loses its nibfin labels, so it takes no new nibfin Job, and is removed after its\n" +
			"Job ends; the command waits for it. Each removal deletes the GitHub registration, the\n" +
			"service and the Runner's directory. A count above the number of Runners is refused.\n\n" +
			"--delete-login deletes the provider's login (the nibfin-home-<provider> volume) only\n" +
			"when no Runner of the provider remains on this host.",
		Args:      providerArgs(1, 1),
		ValidArgs: providers,
		RunE: func(c *cobra.Command, args []string) error {
			// Without --count, Count 0 removes them all.
			if c.Flags().Changed("count") {
				if err := checkCount(removeCount); err != nil {
					return err
				}
			}
			return run(c, ops.RunnerRemove(op.RunnerRemoveParams{Provider: args[0], Count: removeCount, DeleteLogin: deleteLogin}), removeYes)
		},
	}
	runnerRemove.Flags().IntVar(&removeCount, "count", 0, "number of Runners (default: all)")
	runnerRemove.Flags().BoolVar(&deleteLogin, "delete-login", false, "also delete the provider login when no Runner of it remains")
	addYesFlag(runnerRemove, &removeYes)
	runner.AddCommand(runnerAdd, runnerRemove)

	login := &cobra.Command{
		Use:       "login [claude|codex]",
		Short:     "Log in to a provider for the Runners",
		Args:      providerArgs(0, 1),
		ValidArgs: providers,
		RunE: func(c *cobra.Command, args []string) error {
			p := op.LoginParams{}
			if len(args) == 1 {
				p.Provider = args[0]
			}
			p.Terminal, p.Interact = loginIO(stdin, stdout, stderr)
			return run(c, ops.Login(p), false)
		},
	}

	repoCmd := func(use, short string, entry func(op.RepoParams) op.Operation) *cobra.Command {
		var repo string
		var yes bool
		c := &cobra.Command{
			Use:   use,
			Short: short,
			Args:  noArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				r, err := repoResolver(repo)
				if err != nil {
					return err
				}
				return run(c, entry(op.RepoParams{Repo: r}), yes)
			},
		}
		addRepoFlag(c, &repo)
		addYesFlag(c, &yes)
		return c
	}
	initCmd := repoCmd("init", "Set up Nibfin in a repository", func(p op.RepoParams) op.Operation { return ops.Init(p) })
	initCmd.Long = "Set up Nibfin in a repository and open one PR that imports its workflows.\n\n" +
		"It sets the labels scope, shaped, delivered, pause and pending; pull request creation\n" +
		"limited to collaborators; approval required for fork PR workflows (all external\n" +
		"contributors on a public repository, \"Require approval for fork pull request\n" +
		"workflows\" on a private one); \"Automatically delete head branches\" off; the\n" +
		"variables NIBFIN_EXECUTION_REPO (and NIBFIN_PROVIDER in the execution repository);\n" +
		"and the Secret NIBFIN_GITHUB_TOKEN from the current gh login, which needs the\n" +
		"workflow scope (gh auth refresh -s workflow).\n\n" +
		"The PR on branch nibfin/init holds the execution set when this host's Runners serve\n" +
		"the repository, the peer set otherwise. Without Runners it stops and points to\n" +
		"gh nibfin runner add. An initialized repository gets no PR; use gh nibfin upgrade.\n\n" +
		"GitHub offers Issues \"Creation allowed by: Collaborators only\" only in the web UI;\n" +
		"init prints the Settings link to set it there."
	upgradeCmd := repoCmd("upgrade", "Upgrade Nibfin in a repository", func(p op.RepoParams) op.Operation { return ops.Upgrade(p) })

	var statusRepo string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the runner host, logins and the Issues Nibfin works on",
		Long: "Show the runner host, logins and the Issues Nibfin works on. It only reads.\n\n" +
			"Always: the podman machine and its free memory, each Runner's service against\n" +
			"GitHub's online/busy, and each provider's login.\n\n" +
			"Without -R (the person view): your open scope Issues across repositories, with\n" +
			"pause/pending, the pending comment and whether each repository uses the latest image.\n" +
			"With -R (the repository view): all open Issues of that repository, its image and\n" +
			"whether a Nibfin run succeeded after the init PR merged.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			// Without -R the person view needs no repository.
			var r string
			if statusRepo != "" {
				var err error
				if r, err = repoResolver(statusRepo); err != nil {
					return err
				}
			}
			_, res, err := op.ReadBack(c.Context(), ops.Status(op.RepoParams{Repo: r}))
			if err != nil {
				return err
			}
			if *jsonOut {
				writeResult(stdout, res, true)
			} else {
				writeStatus(stdout, res)
			}
			return nil
		},
	}
	statusCmd.Flags().StringVarP(&statusRepo, "repo", "R", "", "show the repository view of [HOST/]OWNER/REPO (default: the person view)")
	addJSONFlag(statusCmd, jsonOut)

	issuesCmd := func(use, short string, entry func(op.IssuesParams) op.Operation) *cobra.Command {
		var repo string
		c := &cobra.Command{
			Use:   use + " <issue>...",
			Short: short,
			Long: short + ".\n\nAn issue is a number (N or #N) in the -R repository, or an Issue URL.\n" +
				"Several issues can be given at once; there is no confirmation step.",
			Args: wrapArgs(cobra.MinimumNArgs(1)),
			RunE: func(c *cobra.Command, args []string) error {
				// Only numbers need a repository, so URL-only calls work
				// outside a checkout.
				var r string
				if repo != "" || op.HasIssueNumber(args) {
					var err error
					if r, err = repoResolver(repo); err != nil {
						return err
					}
				}
				return run(c, entry(op.IssuesParams{Repo: r, Issues: args}), false)
			},
		}
		addRepoFlag(c, &repo)
		return c
	}
	pause := issuesCmd("pause", "Pause Nibfin work on Issues", func(p op.IssuesParams) op.Operation { return ops.Pause(p) })
	resume := issuesCmd("resume", "Resume Nibfin work on Issues", func(p op.IssuesParams) op.Operation { return ops.Resume(p) })

	var port int
	var noBrowser bool
	dashboard := &cobra.Command{
		Use:   "dashboard",
		Short: "Start the local Dashboard",
		Args:  noArgs,
		// dashboard starts the server that serves the Dashboard API, so it
		// has no API equivalent and runs no operation through op.Run.
		RunE: func(*cobra.Command, []string) error {
			return op.NotImplemented("dashboard")
		},
	}
	dashboard.Flags().IntVar(&port, "port", 0, "port to listen on (default: any free port)")
	dashboard.Flags().BoolVar(&noBrowser, "no-browser", false, "do not open a browser")

	root.AddCommand(runner, login, initCmd, upgradeCmd, statusCmd, pause, resume, dashboard)
	root.SetContext(context.Background())
	return root
}

func wrapArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		return op.Wrap(op.CodeInvalidArgument, v(c, args))
	}
}

var noArgs = wrapArgs(cobra.NoArgs)

func providerArgs(min, max int) cobra.PositionalArgs {
	n := cobra.RangeArgs(min, max)
	if min == max {
		n = cobra.ExactArgs(min)
	}
	return wrapArgs(cobra.MatchAll(n, cobra.OnlyValidArgs))
}

func checkCount(n int) error {
	if n < 1 {
		return op.Errorf(op.CodeInvalidArgument, "--count must be at least 1, got %d", n)
	}
	return nil
}

func writeJSON(w io.Writer, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "{\"code\":\"error\",\"message\":%q}\n", err.Error())
		return
	}
	fmt.Fprintln(w, string(b))
}

func writeResult(w io.Writer, r op.Result, asJSON bool) {
	if asJSON {
		if r == nil {
			r = op.Result{}
		}
		writeJSON(w, r)
		return
	}
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var manual map[string]string
	for _, k := range keys {
		if m, ok := r[k].(map[string]string); ok && k == "manual" {
			manual = m
			continue
		}
		fmt.Fprintf(w, "%s: %v\n", k, r[k])
	}
	// A step only the user can take stands on its own lines.
	if manual != nil {
		fmt.Fprintf(w, "\nSet in the web UI: %s\n  %s\n", manual["url"], manual["instruction"])
	}
}
