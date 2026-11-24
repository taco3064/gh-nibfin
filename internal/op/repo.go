package op

import "github.com/cli/go-gh/v2/pkg/repository"

// ResolveRepo turns a repository value into HOST/OWNER/REPO. An empty value
// means the current directory's repository.
func ResolveRepo(value string) (string, error) {
	if value != "" {
		r, err := repository.Parse(value)
		if err != nil {
			return "", Errorf(CodeInvalidRepo, "invalid repository %q: %w", value, err)
		}
		return r.Host + "/" + r.Owner + "/" + r.Name, nil
	}
	r, err := repository.Current()
	if err != nil {
		return "", Errorf(CodeRepoUnresolved, "cannot resolve the current repository: %w", err)
	}
	return r.Host + "/" + r.Owner + "/" + r.Name, nil
}
