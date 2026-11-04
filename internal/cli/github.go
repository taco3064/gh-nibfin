package cli

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

type API interface {
	Call(string, string, any, any) error
}

type GitHub struct{ Process Process }

func (g GitHub) Call(method, path string, input, output any) error {
	args := []string{"api", "--hostname", "github.com", "--method", method,
		"-H", "Accept: application/vnd.github+json", path}
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			return errors.New("INVALID_API_INPUT")
		}
		args = append(args, "--input", "-")
	}
	data, err := g.Process.Run("gh", args, body)
	if err != nil {
		return err
	}
	if raw, ok := output.(*[]byte); ok {
		*raw = data
		return nil
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return errors.New("INVALID_PROVIDER_RESULT")
	}
	return nil
}

type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

type Repository struct {
	ID            int64           `json:"id"`
	FullName      string          `json:"full_name"`
	Private       bool            `json:"private"`
	DefaultBranch string          `json:"default_branch"`
	Owner         User            `json:"owner"`
	Permissions   map[string]bool `json:"permissions"`
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+$`)

func repository(api API, name string, admin bool) (Repository, User, error) {
	var repo Repository
	var user User
	if !repoPattern.MatchString(name) || strings.HasSuffix(name, "/.") ||
		strings.HasSuffix(name, "/..") {
		return repo, user, errors.New("INVALID_REPOSITORY")
	}
	if err := api.Call("GET", "repos/"+name, nil, &repo); err != nil {
		return repo, user, err
	}
	if repo.Private || repo.ID <= 0 || !strings.EqualFold(repo.FullName, name) ||
		(repo.Owner.Type != "User" && repo.Owner.Type != "Organization") {
		return repo, user, errors.New("PUBLIC_REPOSITORY_REQUIRED")
	}
	permission := "push"
	if admin {
		permission = "admin"
	}
	if !repo.Permissions[permission] {
		return repo, user, errors.New("REPOSITORY_PERMISSION_REQUIRED")
	}
	if err := api.Call("GET", "user", nil, &user); err != nil {
		return repo, user, err
	}
	if user.ID <= 0 || user.Login == "" || user.Type != "User" {
		return repo, user, errors.New("AUTHENTICATED_USER_REQUIRED")
	}
	return repo, user, nil
}
