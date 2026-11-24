package op

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
)

// issueRef is one parsed Issue reference.
type issueRef struct {
	Input  string // the reference as given
	Host   string
	Owner  string
	Name   string
	Number int
}

// repoKey identifies the repository; GitHub ignores case in owner and name.
func (r issueRef) repoKey() string {
	return strings.ToLower(r.Host + "/" + r.Owner + "/" + r.Name)
}

// knownHosts lists the hosts gh holds a login or token for. Tests replace it.
var knownHosts = auth.KnownHosts

var issueNumber = regexp.MustCompile(`^#?[0-9]+$`)

// HasIssueNumber reports whether any reference is a number (N or #N), which
// needs a repository to resolve against.
func HasIssueNumber(refs []string) bool {
	for _, r := range refs {
		if issueNumber.MatchString(r) {
			return true
		}
	}
	return false
}

// parseIssueRefs parses numbers (against repo, HOST/OWNER/REPO) and Issue
// URLs. Duplicates collapse to the first one. Every reference must be on one
// host gh is logged in to, since one request goes to one endpoint and the
// token for that host is sent there.
func parseIssueRefs(repo string, refs []string) ([]issueRef, error) {
	if len(refs) == 0 {
		return nil, Errorf(CodeInvalidArgument, "no issue given")
	}
	var out []issueRef
	seen := map[string]bool{}
	for _, in := range refs {
		var r issueRef
		var err error
		if issueNumber.MatchString(in) {
			r, err = parseNumberRef(repo, in)
		} else {
			r, err = parseURLRef(in)
		}
		if err != nil {
			return nil, err
		}
		key := r.repoKey() + "#" + strconv.Itoa(r.Number)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	host := out[0].Host
	for _, r := range out[1:] {
		if r.Host != host {
			return nil, Errorf(CodeInvalidArgument, "issues are on different hosts (%s, %s); give issues from one host", host, r.Host)
		}
	}
	for _, h := range knownHosts() {
		if auth.NormalizeHostname(h) == host {
			return out, nil
		}
	}
	return nil, Errorf(CodeInvalidArgument, "%s is not a host gh is logged in to", host)
}

func parseNumberRef(repo, in string) (issueRef, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(in, "#"))
	if err != nil || n < 1 || n > math.MaxInt32 {
		return issueRef{}, Errorf(CodeInvalidArgument, "invalid issue number %q", in)
	}
	if repo == "" {
		return issueRef{}, Errorf(CodeInvalidArgument, "issue %q is a number but no repository was given", in)
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return issueRef{}, Errorf(CodeInvalidArgument, "invalid repository %q; want HOST/OWNER/REPO", repo)
	}
	return issueRef{Input: in, Host: auth.NormalizeHostname(parts[0]), Owner: parts[1], Name: parts[2], Number: n}, nil
}

// parseURLRef accepts http(s)://HOST/OWNER/REPO/issues/N with an optional
// trailing slash, query or fragment.
func parseURLRef(in string) (issueRef, error) {
	bad := Errorf(CodeInvalidArgument, "invalid issue %q; want a number or an Issue URL", in)
	u, err := url.Parse(in)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Opaque != "" || u.User != nil || u.Port() != "" || u.Hostname() == "" {
		return issueRef{}, bad
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] == "" || parts[2] == "" || parts[3] != "issues" {
		return issueRef{}, bad
	}
	n, err := strconv.Atoi(parts[4])
	if err != nil || strings.Trim(parts[4], "0123456789") != "" || n < 1 || n > math.MaxInt32 {
		return issueRef{}, bad
	}
	return issueRef{Input: in, Host: auth.NormalizeHostname(u.Hostname()), Owner: parts[1], Name: parts[2], Number: n}, nil
}
