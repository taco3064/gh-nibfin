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

// DashboardService is the lifecycle owned by one foreground command.
type DashboardService interface {
	URL() string
	BootstrapURL() string
	Done() <-chan struct{}
	Close() error
}

type DashboardStart func(context.Context, dashboard.Config) (DashboardService, error)

const dashboardUsage = "Usage: gh nibfin dashboard [--repo OWNER/REPO]\n" +
	"Runs in the foreground. Press Ctrl+C to stop; closing a tab does not stop it."

func (a App) runDashboard(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("dashboard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("repo", "", "Repository to read")
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, err = fmt.Fprintln(out, dashboardUsage)
		return err
	}
	if err != nil || flags.NArg() != 0 {
		return errors.New("INVALID_DASHBOARD_ARGUMENTS")
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "repo" })
	if explicit && !snapshot.ValidRepository(*name) {
		return errors.New("INVALID_REPOSITORY")
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	if !explicit {
		*name, err = dashboardRepository(a.Process)
		if err != nil {
			return err
		}
	}
	if err := dashboardIdentity(a.API, *name); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	start := a.StartDashboard
	if start == nil {
		start = func(ctx context.Context, c dashboard.Config) (DashboardService, error) {
			return dashboard.Start(ctx, c)
		}
	}
	service, err := start(ctx, dashboard.Config{Repository: *name,
		Provider: snapshot.Provider{API: a.API}})
	if err != nil {
		return errors.New("DASHBOARD_START_FAILED")
	}
	finish := func(primary error) error {
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

func dashboardRepository(process Process) (string, error) {
	failure := errors.New("DASHBOARD_REPOSITORY_UNAVAILABLE: use --repo OWNER/REPO")
	if process == nil {
		return "", failure
	}
	data, err := process.Run("gh", []string{"repo", "view", "--json", "nameWithOwner"}, nil)
	var result struct {
		Name string `json:"nameWithOwner"`
	}
	if err != nil || len(data) > 1024 || json.Unmarshal(data, &result) != nil ||
		!snapshot.ValidRepository(result.Name) {
		return "", failure
	}
	return result.Name, nil
}

var dashboardLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

func dashboardIdentity(api API, name string) error {
	failure := errors.New("DASHBOARD_AUTHENTICATED_READ_REQUIRED")
	if api == nil {
		return failure
	}
	var user User
	if api.Call("GET", "user", nil, &user) != nil || user.ID <= 0 ||
		user.Type != "User" || !dashboardLogin.MatchString(user.Login) {
		return failure
	}
	var repo Repository
	if api.Call("GET", "repos/"+name, nil, &repo) != nil || repo.ID <= 0 ||
		!snapshot.ValidRepository(repo.FullName) || !strings.EqualFold(repo.FullName, name) ||
		!repo.Permissions["pull"] {
		return failure
	}
	return nil
}
