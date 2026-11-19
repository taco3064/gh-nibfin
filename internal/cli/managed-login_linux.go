package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type selectedGitHub struct {
	ctx    context.Context
	client *http.Client
	token  []byte
	scopes string
}

func readSelectedLogin(ctx context.Context, user User) (*selectedGitHub, error) {
	path, err := toolPath("gh")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "auth", "token", "--hostname",
		"github.com", "--user", user.Login)
	cmd.Env = selectedGhEnvironment(nil)
	var out boundedBuffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("MANAGED_STORED_GH_LOGIN_REQUIRED")
	}
	token := bytes.TrimSpace(out.Bytes())
	if len(token) < 20 || len(token) > 16300 {
		return nil, errors.New("MANAGED_STORED_GH_LOGIN_REQUIRED")
	}
	for _, char := range token {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '_') {
			return nil, errors.New("MANAGED_STORED_GH_LOGIN_REQUIRED")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: 75 * time.Second, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) != 1 || req.URL.Scheme != "https" ||
				req.URL.Hostname() != "artifactcache.actions.githubusercontent.com" &&
					!strings.HasSuffix(req.URL.Hostname(), ".blob.core.windows.net") &&
					!strings.HasSuffix(req.URL.Hostname(), ".githubusercontent.com") {
				return errors.New("MANAGED_GITHUB_REDIRECT_DENIED")
			}
			req.Header.Del("Authorization")
			return nil
		}}
	api := &selectedGitHub{ctx: ctx, client: client, token: token}
	var actual User
	if err := api.Call("GET", "user", nil, &actual); err != nil ||
		actual.ID != user.ID || actual.Type != "User" ||
		!strings.EqualFold(actual.Login, user.Login) {
		clear(token)
		return nil, errors.New("MANAGED_GITHUB_ACCOUNT_CHANGED")
	}
	return api, nil
}

func (g *selectedGitHub) Call(method, path string, input, output any) error {
	var reader io.Reader
	if input != nil {
		body, err := json.Marshal(input)
		if err != nil {
			return errors.New("INVALID_API_INPUT")
		}
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(g.ctx, method,
		"https://api.github.com/"+path, reader)
	if err != nil {
		return errors.New("INVALID_API_INPUT")
	}
	req.Header.Set("Authorization", "Bearer "+string(g.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return errors.New("MANAGED_GITHUB_RESPONSE_UNKNOWN")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return errors.New("NOT_FOUND")
	}
	if resp.StatusCode == 429 || resp.StatusCode == 403 &&
		resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return errors.New("RATE_LIMITED")
	}
	if resp.StatusCode == 403 {
		return errors.New("PERMISSION_DENIED")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("MANAGED_GITHUB_RESPONSE_UNKNOWN")
	}
	if path == "user" {
		g.scopes = resp.Header.Get("X-OAuth-Scopes")
	}
	if output == nil {
		return nil
	}
	const limit = 40 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(data) > limit {
		return errors.New("INVALID_PROVIDER_RESULT")
	}
	if raw, ok := output.(*[]byte); ok {
		*raw = data
	} else if json.Unmarshal(data, output) != nil {
		return errors.New("INVALID_PROVIDER_RESULT")
	}
	return nil
}
