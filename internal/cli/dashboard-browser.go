package cli

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

func browserCommand(platform, target string) (string, []string, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "http" || u.User != nil ||
		u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "/" ||
		u.RawQuery != "" || len(u.Fragment) != 64 {
		return "", nil, errors.New("INVALID_DASHBOARD_URL")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return "", nil, errors.New("INVALID_DASHBOARD_URL")
	}
	switch platform {
	case "windows":
		return "explorer.exe", []string{target}, nil
	case "darwin":
		return "open", []string{target}, nil
	case "linux":
		return "xdg-open", []string{target}, nil
	default:
		return "", nil, errors.New("BROWSER_UNAVAILABLE")
	}
}

func openDashboard(ctx context.Context, target string) error {
	name, args, err := browserCommand(runtime.GOOS, target)
	if err != nil {
		return err
	}
	path, err := toolPath(name)
	if err != nil {
		return errors.New("BROWSER_UNAVAILABLE")
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(bounded, path, args...)
	// Browser helpers require the user's desktop environment. Output is discarded.
	command.WaitDelay = time.Second
	if command.Run() != nil {
		return errors.New("BROWSER_UNAVAILABLE")
	}
	return nil
}
