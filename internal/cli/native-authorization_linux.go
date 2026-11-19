package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func selectedLoginEnvironment() []string {
	environment := []string{"PATH=/usr/bin:/bin", "GH_PROMPT_DISABLED=1",
		"GH_NO_UPDATE_NOTIFIER=1", "GH_NO_EXTENSION_UPDATE_NOTIFIER=1"}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR",
		"DBUS_SESSION_BUS_ADDRESS", "GH_CONFIG_DIR", "GNUPGHOME"} {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

// gh refresh operates on the active stored account, so verify it independently
// before allowing that operation. Never switch logins to obtain authorization.
func refreshSelectedAuthorization(ctx context.Context, selected User) error {
	path, err := toolPath("gh")
	if err != nil {
		return err
	}
	active := exec.CommandContext(ctx, path, "api", "--hostname", "github.com", "user")
	active.Env = selectedLoginEnvironment()
	var output boundedBuffer
	active.Stdout = &output
	var observed User
	if active.Run() != nil || json.Unmarshal(output.Bytes(), &observed) != nil ||
		observed.ID != selected.ID || observed.Type != "User" ||
		!strings.EqualFold(observed.Login, selected.Login) {
		return errors.New("MANAGED_REFRESH_REQUIRES_SELECTED_ACTIVE_ACCOUNT")
	}
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("MANAGED_AUTHORIZATION_REQUIRES_TERMINAL")
	}
	defer terminal.Close()
	refresh := exec.CommandContext(ctx, path, "auth", "refresh", "--hostname",
		"github.com", "--scopes", "repo,workflow")
	for _, item := range selectedLoginEnvironment() {
		if !strings.HasPrefix(item, "GH_PROMPT_DISABLED=") {
			refresh.Env = append(refresh.Env, item)
		}
	}
	refresh.Stdin, refresh.Stdout, refresh.Stderr = terminal, terminal, terminal
	if refresh.Run() != nil {
		return errors.New("MANAGED_AUTHORIZATION_REFRESH_UNCONFIRMED")
	}
	return nil
}
