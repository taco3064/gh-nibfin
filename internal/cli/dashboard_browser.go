package cli

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func dashboardBrowserCommand(platform, target string) (string, []string, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" ||
		u.Path != "/" || len(u.Fragment) != 64 {
		return "", nil, errors.New("INVALID_DASHBOARD_HANDOFF")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return "", nil, errors.New("INVALID_DASHBOARD_HANDOFF")
	}
	switch platform {
	case "linux":
		return "xdg-open", []string{target}, nil
	case "darwin":
		return "open", []string{target}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}, nil
	default:
		return "", nil, errors.New("DASHBOARD_BROWSER_UNAVAILABLE")
	}
}

func openDashboardBrowser(parent context.Context, target string) error {
	name, args, err := dashboardBrowserCommand(runtime.GOOS, target)
	if err != nil {
		return err
	}
	path, err := toolPath(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	for _, key := range []string{"PATH", "HOME", "USERPROFILE", "SystemRoot",
		"WINDIR", "TEMP", "TMP", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR",
		"DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LOCALAPPDATA", "APPDATA"} {
		if value, ok := os.LookupEnv(key); ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	return cmd.Run()
}
