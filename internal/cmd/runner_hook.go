package cmd

import (
	"context"
	"os/exec"

	"github.com/taco3064/gh-nibfin/internal/image"
	"github.com/taco3064/gh-nibfin/internal/op"
	"github.com/taco3064/gh-nibfin/internal/runnerhook"
	"github.com/taco3064/gh-nibfin/internal/templates"
)

// runner add installs the job-started hook of a collaborator's Runner from
// runnerhook's files, in the CLI and the Dashboard alike.
func init() { op.SetRunnerHookFiles(runnerHookFiles) }

// Tests replace these.
var (
	hookImage  = image.Latest
	hookRender = templates.Render
	hookFiles  = runnerhook.Files
)

// runnerHookFiles builds the hook's constant files. The official workflow
// files are the user execution set that `nibfin render-workflows` in the
// latest attested image renders on runner add's host, with the placeholder
// image digest and Action commit the hook puts in a repository's files
// before comparing. The branch and CI workflow names change only
// nibfin-issue.yml, which the hook does not compare.
func runnerHookFiles(ctx context.Context, podman func(context.Context, ...string) *exec.Cmd) (map[string][]byte, error) {
	ref, err := hookImage(ctx)
	if err != nil {
		return nil, err
	}
	rendered, err := hookRender(ctx, podman, templates.Execution, templates.Params{
		Run:           ref,
		Image:         runnerhook.PlaceholderImage,
		Commit:        runnerhook.PlaceholderCommit,
		DefaultBranch: "main",
	})
	if err != nil {
		return nil, err
	}
	return hookFiles(ctx, rendered)
}
