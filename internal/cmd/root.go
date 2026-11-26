// Package cmd is the gh nibfin command tree. Each handler maps its arguments
// onto one entry point of the shared operation layer: internal/op, or a
// package of that layer such as internal/upgrade.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/initrepo"
	"github.com/taco3064/gh-nibfin/internal/live"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/runnerhook"
	"github.com/taco3064/gh-nibfin/internal/status"
	"github.com/taco3064/gh-nibfin/internal/upgrade"
)

// ops holds the operation entry points the handlers and the Dashboard API
// call. Tests replace it.
var ops = dashboard.Ops{
	RunnerAdd:    op.RunnerAdd,
	RunnerRemove: op.RunnerRemove,
	Login:        op.Login,
	Init:         initrepo.New,
	Upgrade:      upgrade.New,
	Status:       status.New,
	Pause:        op.Pause,
	Resume:       op.Resume,
}

// runner add installs the job-started hook of a collaborator's Runner from
// runnerhook's files, in the CLI and the Dashboard alike.
func init() { op.SetRunnerHookFiles(runnerhook.Files) }

var providers = []string{"claude", "codex"}

// providerArg declares the provider positional of runner add, runner remove
// and login.
func providerArg(required bool) dashboard.Arg {
	return dashboard.Arg{Name: "provider", Required: required, Allowed: providers, Param: "provider"}
}

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
		// Each item's progress is one line on stderr.
		ctx := op.WithProgress(c.Context(), func(e op.Event) { fmt.Fprintln(stderr, progressLine(e)) })
		_, r, err := op.Run(ctx, o, confirmer{yes: yes, stdin: stdin, stderr: stderr})
		// The items read back are printed also when the read-back fails.
		if r != nil {
			writeResult(stdout, r, false)
		}
		return err
	}

	runner := &cobra.Command{
		Use:   "runner",
		Short: "Add or remove local Runners",
		// Without RunE cobra shows help and exits 0 for an unknown
		// subcommand, so a missing one fails
		// here and the declared NoArgs refuses an unknown one.
		RunE: func(c *cobra.Command, _ []string) error {
			return op.Errorf(op.CodeInvalidArgument, "%q requires a subcommand: add or remove", c.CommandPath())
		},
	}
	declareArgs(runner)

	var addCount int
	var addYes bool
	runnerAdd := &cobra.Command{
		Use:   "add <claude|codex>",
		Short: "Add Runners for a provider on this host",
		Long: "Add Runners for a provider on this host (the Linux host, or the podman machine VM).\n\n" +
			"The first Runner on the host registers to the current directory's repository; later\n" +
			"Runners reuse the repository the existing Runners use. Each Runner carries the label\n" +
			"nibfin-<user id> (and nibfin when you are admin of the repository), names its provider\n" +
			"as NIBFIN_PROVIDER in its .env, and runs as a systemd user service.\n\n" +
			"A Runner's .env may also set NIBFIN_MODEL=<model> for that Runner only; without it,\n" +
			"Core's default for the provider applies. runner add does not write that line, and\n" +
			"runner add and upgrade keep it. Restart the Runner after changing its .env. An old\n" +
			"NIBFIN_MODEL repository variable is unused and can be deleted.\n\n" +
			"An admin registers directly. A collaborator with write access gets the registration\n" +
			"token through a \"Nibfin runner request\" Issue, encrypted to an SSH key of their GitHub\n" +
			"account that is in ~/.ssh; the command waits up to 10 minutes for the answer. On a\n" +
			"private repository the request also carries a read-only deploy key, created once per\n" +
			"repository at ~/.local/share/nibfin/deploy-keys/<host>/<owner>/<name>/id_ed25519\n" +
			"(under XDG_DATA_HOME when set); the owner removes the deploy key nibfin-runner-<login>\n" +
			"under Settings > Deploy keys together with your Runners.\n\n" +
			"A collaborator's Runners get a job-started hook in ~/.local/share/nibfin/hook (under\n" +
			"XDG_DATA_HOME when set), so they run only official Nibfin Jobs for you. The hook checks\n" +
			"that nibfin-shared-runner.yml started the Job for your user id, that it and\n" +
			"nibfin-job.yml at that commit match this gh-nibfin's templates except the image digest,\n" +
			"and that the image's Sigstore signature and signer (nibfin-core's image.yml on main)\n" +
			"verify on the host. A refused Job fails before any step, with the reason under \"Set up\n" +
			"runner\" in its log. A private repository is read with the deploy key and needs git and\n" +
			"ssh on the runner host. Each Job reads the attestation API without a credential (60\n" +
			"requests per hour per IP). After the owner upgrades the repository, or Sigstore's root\n" +
			"changes, update gh-nibfin and run runner add again.",
		RunE: func(c *cobra.Command, args []string) error {
			if err := checkCount(addCount); err != nil {
				return err
			}
			p := op.RunnerAddParams{Provider: args[0], Count: addCount}
			p.Terminal, p.Interact = loginIO(stdin, stdout, stderr)
			return run(c, ops.RunnerAdd(p), addYes)
		},
	}
	declareArgs(runnerAdd, providerArg(true))
	declareAPI(runnerAdd, "runner-add")
	runnerAdd.Flags().IntVar(&addCount, "count", op.DefaultRunnerAddCount, "number of Runners")
	declareParam(runnerAdd, "count", "count")
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
			"When you are not admin of the Runners' repository (a collaborator's Runners), the removal\n" +
			"is local: it waits for a running Job, then deletes the service and the directory only.\n" +
			"The GitHub registration stays; the repository's owner deletes it under Settings → Actions →\n" +
			"Runners, and GitHub removes a runner offline for more than 14 days.\n\n" +
			"When the host's last Runner of any provider goes, the job-started hook of a\n" +
			"collaborator's Runners goes with it.\n\n" +
			"--delete-login deletes the provider's login (the nibfin-home-<provider> volume) only\n" +
			"when no Runner of the provider remains on this host.",
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
	declareArgs(runnerRemove, providerArg(true))
	declareAPI(runnerRemove, "runner-remove")
	runnerRemove.Flags().IntVar(&removeCount, "count", 0, "number of Runners (default: all)")
	declareParam(runnerRemove, "count", "count")
	runnerRemove.Flags().BoolVar(&deleteLogin, "delete-login", false, "also delete the provider login when no Runner of it remains")
	declareParam(runnerRemove, "delete-login", "deleteLogin")
	addYesFlag(runnerRemove, &removeYes)
	runner.AddCommand(runnerAdd, runnerRemove)

	login := &cobra.Command{
		Use:   "login [claude|codex]",
		Short: "Log in to a provider for the Runners",
		RunE: func(c *cobra.Command, args []string) error {
			p := op.LoginParams{}
			if len(args) == 1 {
				p.Provider = args[0]
			}
			p.Terminal, p.Interact = loginIO(stdin, stdout, stderr)
			return run(c, ops.Login(p), false)
		},
	}
	declareArgs(login, providerArg(false))
	declareAPI(login, "login")

	repoCmd := func(use, short string, entry func(op.RepoParams) op.Operation) *cobra.Command {
		var repo string
		var yes bool
		c := &cobra.Command{
			Use:   use,
			Short: short,
			RunE: func(c *cobra.Command, _ []string) error {
				r, err := repoResolver(repo)
				if err != nil {
					return err
				}
				return run(c, entry(op.RepoParams{Repo: r}), yes)
			},
		}
		declareArgs(c)
		declareAPI(c, use)
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
		"variable NIBFIN_EXECUTION_REPO; and the Secret NIBFIN_GITHUB_TOKEN from the current\n" +
		"gh login, which needs the workflow scope (gh auth refresh -s workflow). It neither\n" +
		"writes nor reads NIBFIN_PROVIDER: each Runner names its provider in its .env.\n\n" +
		"The PR on branch nibfin/init holds the execution set when this host's Runners serve\n" +
		"the repository, the peer set otherwise. Without Runners it stops and points to\n" +
		"gh nibfin runner add. For the peer set the gh login must be admin of the Runners'\n" +
		"repository too; otherwise init stops before any write. An initialized repository\n" +
		"gets no PR; use gh nibfin upgrade.\n\n" +
		"GitHub offers Issues \"Creation allowed by: Collaborators only\" only in the web UI;\n" +
		"init prints the Settings link to set it there."
	upgradeCmd := repoCmd("upgrade", "Upgrade Nibfin in a repository", func(p op.RepoParams) op.Operation { return ops.Upgrade(p) })
	upgradeCmd.Long = "Upgrade Nibfin in a repository and bring this host's Runners to the current spec.\n\n" +
		"The repository part renders the workflows from the current templates, pinned to\n" +
		"the latest attested image, and opens one PR on branch nibfin/upgrade when they\n" +
		"differ from the default branch.\n\n" +
		"The host part runs whether or not the workflows are current. Each configured\n" +
		"Runner on this host gets what runner add sets up today: NIBFIN_PROVIDER in its\n" +
		".env (other lines, such as NIBFIN_MODEL, are kept), the current systemd user unit, enabled and active, and, when the gh login\n" +
		"is admin of the Runners' repository, the labels nibfin and nibfin-<user id>.\n" +
		"Without admin the labels are left as they are. Runners are changed one at a\n" +
		"time; a Runner running a Job is restarted only after the Job ends, so the\n" +
		"others keep taking work. A stopped podman machine is not started."

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
	declareArgs(statusCmd)
	declareAPI(statusCmd, "status")
	statusCmd.Flags().StringVarP(&statusRepo, "repo", "R", "", "show the repository view of [HOST/]OWNER/REPO (default: the person view)")
	declareParam(statusCmd, "repo", "repo")
	addJSONFlag(statusCmd, jsonOut)

	issuesCmd := func(use, short string, entry func(op.IssuesParams) op.Operation) *cobra.Command {
		var repo string
		c := &cobra.Command{
			Use:   use + " <issue>...",
			Short: short,
			Long: short + ".\n\nAn issue is a number (N or #N) in the -R repository, or an Issue URL.\n" +
				"Several issues can be given at once; there is no confirmation step.",
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
		declareArgs(c, dashboard.Arg{Name: "issue", Required: true, Repeatable: true, Param: "issues"})
		declareAPI(c, use)
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
		Long: "Start the local Dashboard server in the foreground; Ctrl-C stops it.\n\n" +
			"It listens on 127.0.0.1 only and opens your browser on a session URL. The proof in\n" +
			"that URL works once: the first page or client that opens it gets the run's only\n" +
			"session, with the same access as your gh login on this machine, and the terminal\n" +
			"prints \"session opened\". Keep the URL private until then; restart for a new one.\n" +
			"--no-browser prints the session URL instead of opening a browser.\n\n" +
			"The page is the Dashboard UI built from dashboard/; a binary built without it serves\n" +
			"a placeholder page. The API is under /api/.",
		// dashboard starts the server that serves the Dashboard API, so it
		// has no API equivalent and runs no operation through op.Run.
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			// Ready, the browser goroutine and SessionOpened write concurrently.
			var mu sync.Mutex
			out, errOut := syncWriter{&mu, stdout}, syncWriter{&mu, stderr}
			return dashboardServe(ctx, dashboard.Options{
				Port:        port,
				Ops:         ops,
				ResolveRepo: repoResolver,
				Ready: func(sessionURL string) {
					if noBrowser {
						fmt.Fprintf(out, "Dashboard session URL (works once; Ctrl-C stops the server):\n%s\n", sessionURL)
						return
					}
					base, _, _ := strings.Cut(sessionURL, "#")
					fmt.Fprintf(out, "Dashboard: %s (opening your browser; Ctrl-C stops it)\n", base)
					// The launcher is waited on; serving must not wait for it.
					go func() {
						if err := openBrowser(sessionURL, out, errOut); err != nil {
							fmt.Fprintf(out, "cannot open a browser: %v\nopen the session URL (works once):\n%s\n", err, sessionURL)
						}
					}()
				},
				SessionOpened: func() { fmt.Fprintln(out, "session opened") },
				Commands:      commandSchema(c.Root()),
				Watch:         watchPerson,
				Activity:      readActivity(),
			})
		},
	}
	declareArgs(dashboard)
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
	items, _ := r[op.ResultItems].([]op.Check)
	notes, hasNotes := r[op.ResultNotes].([]string)
	keys := make([]string, 0, len(r))
	for k := range r {
		switch {
		case k == op.ResultItems, k == op.ResultNotes:
		case k == "advice" && hasNotes:
			// The advice is printed as the notes.
		default:
			keys = append(keys, k)
		}
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
	writeChecks(w, items)
	// A step only the user can take stands on its own lines; it is the
	// note, so the notes are not repeated.
	if manual != nil {
		fmt.Fprintf(w, "\nSet in the web UI: %s\n  %s\n", manual["url"], manual["instruction"])
		return
	}
	writeNotes(w, notes)
}

// watchPerson keeps the Dashboard's person view current over the same
// status entry point the CLI calls.
func watchPerson(ctx context.Context, send func(any)) error {
	return live.Watch(ctx, live.Deps{Status: ops.Status, ReadIssue: status.ReadIssue}, func(e live.Event) { send(e) })
}

// dashboardServe runs the Dashboard server. Tests replace it.
var dashboardServe = dashboard.Serve

// openBrowser opens url with gh's browser launcher (GH_BROWSER, the gh
// browser setting, BROWSER, then the platform's default). Tests replace it.
var openBrowser = func(url string, stdout, stderr io.Writer) error {
	return browser.New("", stdout, stderr).Browse(url)
}

// syncWriter serialises writes that share mu.
type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (s syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// readActivity returns the Runner activity read of one dashboard command:
// one reader, whose in-memory cache lives as long as the server.
func readActivity() func(context.Context) (any, error) {
	a := status.NewActivityReader()
	return func(ctx context.Context) (any, error) { return a.Read(ctx) }
}
