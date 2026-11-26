// Package workflowfiles embeds the workflow templates gh nibfin writes into
// a user repository and renders one of them. It imports nothing of gh
// nibfin, so both internal/templates (which adds validation and error codes)
// and the collaborator runner hook (which compares a repository's files with
// the official ones) render the same bytes.
package workflowfiles

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed workflows/*.yml
var files embed.FS

// parsed holds the templates. The delimiters [[ ]] let GitHub's ${{ }} and
// Podman's {{.ID}} pass through unchanged.
var parsed = template.Must(template.New("").Delims("[[", "]]").Option("missingkey=error").ParseFS(files, "workflows/*.yml"))

// Render returns the template name (a file name such as nibfin-job.yml)
// rendered with data. A missing value or an unknown name is an error.
func Render(name string, data map[string]string) ([]byte, error) {
	if parsed.Lookup(name) == nil {
		return nil, fmt.Errorf("render %s: no such template", name)
	}
	var b bytes.Buffer
	if err := parsed.ExecuteTemplate(&b, name, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return b.Bytes(), nil
}
