// Package templates holds the workflow templates gh nibfin writes into a user
// repository and renders them. The templates mirror Core's workflows but call
// nothing in the Owner's repositories: the Job runs the Nibfin image pinned by
// digest.
package templates

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"unicode"

	"github.com/taco3064/gh-nibfin/internal/op"
)

//go:embed workflows/*.yml
var files embed.FS

// Set selects which workflow files a repository gets.
type Set int

const (
	// Execution is the set of the execution repository, which runs every Job.
	Execution Set = iota
	// Peer is the set of a repository whose events wake the execution repository.
	Peer
)

// Dir is where the rendered files go in the target repository.
const Dir = ".github/workflows"

var setFiles = map[Set][]string{
	Execution: {"nibfin-issue.yml", "nibfin-shared-runner.yml", "nibfin-job.yml"},
	Peer:      {"nibfin-issue.yml"},
}

// Params are the values a template is rendered with.
type Params struct {
	// Image is the pinned image, ghcr.io/taco3064/nibfin@sha256:<digest>.
	Image string
	// DefaultBranch is the target repository's default branch.
	DefaultBranch string
	// Workflows are the names of the target repository's CI/CD workflows.
	Workflows []string
}

var imagePattern = regexp.MustCompile(`^ghcr\.io/taco3064/nibfin@sha256:[0-9a-f]{64}$`)

// parsed holds the templates. The delimiters [[ ]] let GitHub's ${{ }} and
// Podman's {{.ID}} pass through unchanged.
var parsed = template.Must(template.New("").Delims("[[", "]]").Option("missingkey=error").ParseFS(files, "workflows/*.yml"))

// Render returns the files of set, keyed by their path in the repository.
// The output is deterministic: the same input gives the same bytes.
func Render(set Set, p Params) (map[string][]byte, error) {
	names, ok := setFiles[set]
	if !ok {
		return nil, op.Errorf(op.CodeInvalidTemplateInput, "unknown template set %d", set)
	}
	data, err := values(p)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(names))
	for _, name := range names {
		var b bytes.Buffer
		if err := parsed.ExecuteTemplate(&b, name, data); err != nil {
			return nil, op.Wrap(op.CodeInvalidTemplateInput, fmt.Errorf("render %s: %w", name, err))
		}
		out[path.Join(Dir, name)] = b.Bytes()
	}
	return out, nil
}

// values validates p and turns it into the strings the templates insert.
func values(p Params) (map[string]string, error) {
	if !imagePattern.MatchString(p.Image) {
		return nil, op.Errorf(op.CodeInvalidTemplateInput, "image %q is not ghcr.io/taco3064/nibfin@sha256:<digest>", p.Image)
	}
	if p.DefaultBranch == "" || strings.ContainsAny(p.DefaultBranch, "\"'") || hasControl(p.DefaultBranch) {
		return nil, op.Errorf(op.CodeInvalidTemplateInput, "default branch %q is empty or has a quote or control character", p.DefaultBranch)
	}
	names := slices.Clone(p.Workflows)
	for _, n := range names {
		if strings.TrimSpace(n) == "" || hasControl(n) {
			return nil, op.Errorf(op.CodeInvalidTemplateInput, "workflow name %q is empty or has a control character", n)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	workflows := ""
	if len(names) > 0 {
		workflows = flow(names)
	}
	return map[string]string{
		"Image":     p.Image,
		"Branches":  flow([]string{p.DefaultBranch}),
		"Workflows": workflows,
	}, nil
}

// flow writes items as a YAML flow sequence of JSON-quoted strings. JSON string
// escapes are valid YAML double-quoted escapes.
func flow(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(s) // a string always encodes
		quoted[i] = strings.TrimSuffix(b.String(), "\n")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, unicode.IsControl)
}
