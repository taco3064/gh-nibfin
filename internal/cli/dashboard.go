package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/taco3064/gh-nibfin/internal/dashboard"
	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

const dashboardHelp = "Usage: gh nibfin dashboard [--repo OWNER/REPO]\n" +
	"Uses your GitHub login and read access; defaults to the current repository.\n" +
	"Opens a local dashboard and stays in the foreground. Press Ctrl+C to stop.\n" +
	"Shutdown waits for bounded in-flight reads; closing the tab does not stop it."

// DashboardSession is the lifecycle of the existing loopback service.
type DashboardSession interface {
	URL() string
	BootstrapURL() string
	Done() <-chan struct{}
	Close() error
}

// DashboardRuntime supplies trusted local adapters, never browser input.
type DashboardRuntime struct {
	Start func(context.Context, dashboard.Config) (DashboardSession, error)
	Open  func(context.Context, string) error
}

func (a App) runDashboard(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(out, dashboardHelp)
		return err
	}
	flags := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "OWNER/REPO")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return errors.New("INVALID_DASHBOARD_ARGUMENTS")
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "repo" })
	if ctx.Err() != nil {
		return nil
	}
	if !explicit {
		if a.Process == nil {
			return errors.New("REPOSITORY_UNRESOLVED_USE_--repo")
		}
		data, err := a.Process.Run("gh", []string{"repo", "view", "--json",
			"nameWithOwner"}, nil)
		var result struct {
			Name string `json:"nameWithOwner"`
		}
		if err != nil || len(data) > 1024 || json.Unmarshal(data, &result) != nil ||
			!snapshot.ValidRepository(result.Name) {
			return errors.New("REPOSITORY_UNRESOLVED_USE_--repo")
		}
		*name = result.Name
	}
	if !snapshot.ValidRepository(*name) {
		return errors.New("INVALID_REPOSITORY")
	}
	if err := dashboardIdentity(a.API, *name); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	runtime := DashboardRuntime{Start: func(ctx context.Context, c dashboard.Config) (
		DashboardSession, error) {
		return dashboard.Start(ctx, c)
	}, Open: openDashboard}
	if a.Dashboard != nil {
		runtime = *a.Dashboard
	}
	service, err := runtime.Start(ctx, dashboard.Config{Repository: *name,
		Provider: snapshot.Provider{API: a.API}})
	if err != nil {
		return errors.New("DASHBOARD_START_FAILED")
	}
	finish := func(cause error) error {
		if service.Close() != nil {
			return errors.New("DASHBOARD_SHUTDOWN_FAILED")
		}
		return cause
	}
	if ctx.Err() != nil {
		return finish(nil)
	}
	select {
	case <-service.Done():
		return finish(nil)
	default:
	}
	bootstrap := service.BootstrapURL()
	if bootstrap == "" {
		return finish(errors.New("DASHBOARD_SESSION_UNAVAILABLE"))
	}
	if runtime.Open(ctx, bootstrap) != nil {
		return finish(errors.New("DASHBOARD_BROWSER_OPEN_FAILED"))
	}
	if _, err := fmt.Fprintln(out, "Dashboard: "+service.URL()+
		"\nPress Ctrl+C to stop; shutdown waits for bounded in-flight reads."); err != nil {
		return finish(errors.New("OUTPUT_UNAVAILABLE"))
	}
	select {
	case <-ctx.Done():
	case <-service.Done():
	}
	return finish(nil)
}

var dashboardLoginPattern = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

func dashboardIdentity(api API, name string) error {
	if api == nil {
		return errors.New("DASHBOARD_LOGIN_REQUIRED")
	}
	var user User
	if api.Call("GET", "user", nil, &user) != nil || user.ID <= 0 ||
		user.Type != "User" || len(user.Login) > 39 ||
		!dashboardLoginPattern.MatchString(user.Login) {
		return errors.New("DASHBOARD_LOGIN_REQUIRED")
	}
	var repo Repository
	if api.Call("GET", "repos/"+name, nil, &repo) != nil || repo.ID <= 0 ||
		!snapshot.ValidRepository(repo.FullName) || !strings.EqualFold(repo.FullName, name) ||
		!repo.Permissions["pull"] {
		return errors.New("DASHBOARD_REPOSITORY_READ_REQUIRED")
	}
	return nil
}
