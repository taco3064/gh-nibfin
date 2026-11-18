package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/pause"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// DashboardService is the lifecycle owned by one foreground command.
type DashboardService interface {
	URL() string
	BootstrapURL() string
	Done() <-chan struct{}
	Close() error
}

type DashboardStart func(context.Context, dashboard.Config) (DashboardService, error)

const dashboardUsage = "Usage: gh nibfin dashboard [--overview] [--target wsl:NAME]\n" +
	"Open your overview and select a project in the page.\n" +
	"Runs in the foreground. Press Ctrl+C to stop; closing a tab does not stop it."

func (a App) runDashboard(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Bool("overview", false, "Compatibility synonym for the user overview")
	target := flags.String("target", "", "Existing running WSL overview target")
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--repo" || arg == "-repo" ||
			strings.HasPrefix(arg, "--repo=") || strings.HasPrefix(arg, "-repo=") {
			return errors.New("DASHBOARD_REPO_REMOVED: run gh nibfin dashboard " +
				"and select a project in the page")
		}
	}
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, err = fmt.Fprintln(out, dashboardUsage)
		return err
	}
	if err != nil || flags.NArg() != 0 {
		return errors.New("INVALID_DASHBOARD_ARGUMENTS")
	}
	targetSet := false
	flags.Visit(func(f *flag.Flag) { targetSet = targetSet || f.Name == "target" })
	targetErr := validateOverviewTarget(*target, targetSet, runtime.GOOS)
	if targetErr != nil {
		return targetErr
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	subject, err := dashboardSubject(a.API)
	if err != nil {
		return err
	}
	repositoryProvider := dashboardOverviewRepository{api: a.API, subject: subject}
	diagnoser := dashboardDiagnoser{api: a.API, process: a.Process, subject: subject}
	var observer snapshot.LocalObserver = dashboardObserver{
		process: a.Process, subject: subject}
	if runtime.GOOS == "windows" {
		observer = &wslOverviewObserver{ctx: ctx, process: a.Process,
			subject: subject, explicit: *target}
	}
	overviewProvider := snapshot.OverviewProvider{Provider: snapshot.Provider{API: a.API},
		Subject: subject, Observer: observer}
	if ctx.Err() != nil {
		return nil
	}
	start := a.StartDashboard
	if start == nil {
		start = func(ctx context.Context, c dashboard.Config) (DashboardService, error) {
			return dashboard.Start(ctx, c)
		}
	}
	service, err := start(ctx, dashboard.Config{
		Provider: repositoryProvider, Overview: overviewProvider, Diagnoser: diagnoser,
		Controller: pause.Controller{Provider: NativePause{API: a.API, Process: a.Process}}})
	if err != nil {
		return errors.New("DASHBOARD_START_FAILED")
	}
	finish := func(primary error) error {
		cancel()
		if service.Close() != nil {
			return errors.New("DASHBOARD_SHUTDOWN_FAILED")
		}
		return primary
	}
	if ctx.Err() != nil {
		return finish(nil)
	}
	bootstrap := service.BootstrapURL()
	if bootstrap == "" {
		return finish(errors.New("DASHBOARD_SESSION_UNAVAILABLE"))
	}
	open := a.OpenDashboard
	if open == nil {
		open = openDashboardBrowser
	}
	if err := open(ctx, bootstrap); err != nil {
		return finish(errors.New("DASHBOARD_BROWSER_OPEN_FAILED"))
	}
	if _, err := fmt.Fprintln(out, "Dashboard: "+service.URL()+
		"\nPress Ctrl+C to stop (bounded shutdown may wait for active reads)."); err != nil {
		return finish(errors.New("OUTPUT_UNAVAILABLE"))
	}
	select {
	case <-ctx.Done():
	case <-service.Done():
	}
	return finish(nil)
}

var dashboardLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

func validateOverviewTarget(target string, set bool, host string) error {
	if set && (host != "windows" ||
		!strings.HasPrefix(target, "wsl:") ||
		!guestName.MatchString(strings.TrimPrefix(target, "wsl:"))) {
		return errors.New("INVALID_DASHBOARD_TARGET")
	}
	return nil
}
