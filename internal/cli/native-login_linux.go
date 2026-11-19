package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
)

func selectedGhEnvironment(token []byte) []string {
	env := []string{"PATH=/usr/bin:/bin", "GH_NO_UPDATE_NOTIFIER=1",
		"GH_NO_EXTENSION_UPDATE_NOTIFIER=1"}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR",
		"DBUS_SESSION_BUS_ADDRESS", "GH_CONFIG_DIR", "GNUPGHOME"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	if len(token) != 0 {
		env = append(env, "GH_TOKEN="+string(token), "GH_PROMPT_DISABLED=1")
	}
	return env
}

func workflowScope(scopes string) bool {
	// Fine-grained tokens need effective permission checks, not OAuth headers.
	if scopes == "" {
		return true
	}
	for _, scope := range strings.Split(scopes, ",") {
		if strings.TrimSpace(scope) == "workflow" {
			return true
		}
	}
	return false
}

func activeSelectedAccount(ctx context.Context, path string, user User) bool {
	cmd := exec.CommandContext(ctx, path, "api", "--hostname", "github.com", "user")
	cmd.Env = selectedGhEnvironment(nil)
	var out boundedBuffer
	cmd.Stdout = &out
	var observed User
	return cmd.Run() == nil && json.Unmarshal(out.Bytes(), &observed) == nil &&
		observed.ID == user.ID && observed.Type == "User" &&
		strings.EqualFold(observed.Login, user.Login)
}

func selectedLogin(ctx context.Context, user User) (*selectedGitHub, error) {
	api, err := readSelectedLogin(ctx, user)
	if err != nil || workflowScope(api.scopes) {
		return api, err
	}
	clear(api.token)
	path, err := toolPath("gh")
	if err != nil || !activeSelectedAccount(ctx, path, user) {
		return nil, errors.New("MANAGED_GITHUB_ACCOUNT_CHANGED")
	}
	// Use GitHub CLI's documented browser/device authorization; never ask for
	// manual token creation or switch the user's active account.
	cmd := exec.CommandContext(ctx, path, "auth", "refresh", "--hostname",
		"github.com", "--scopes", "workflow")
	cmd.Env = selectedGhEnvironment(nil)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if cmd.Run() != nil || !activeSelectedAccount(ctx, path, user) {
		return nil, errors.New("MANAGED_GH_AUTHORIZATION_REFRESH_REQUIRED")
	}
	api, err = readSelectedLogin(ctx, user)
	if err != nil {
		return nil, err
	}
	if !workflowScope(api.scopes) {
		clear(api.token)
		return nil, errors.New("MANAGED_GH_AUTHORIZATION_REFRESH_REQUIRED")
	}
	return api, nil
}

func (g *selectedGitHub) publishNativeWakeSecret(repo string) error {
	if !repoPattern.MatchString(repo) {
		return errors.New("INVALID_REPOSITORY")
	}
	path, err := toolPath("gh")
	if err != nil {
		return err
	}
	cmd := nativeSecretCommand(g.ctx, path, repo, g.token)
	if cmd.Run() != nil {
		return errors.New("NATIVE_WAKE_SECRET_WRITE_UNKNOWN")
	}
	return nil
}

func nativeSecretCommand(ctx context.Context, path, repo string, token []byte) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, "secret", "set", nativeWakeSecret,
		"--app", "actions", "--repo", repo)
	// gh encrypts with the repository public key. The selected credential stays
	// in private stdin and the child's private environment, never command args.
	cmd.Env = selectedGhEnvironment(token)
	cmd.Stdin = bytes.NewReader(token)
	return cmd
}
