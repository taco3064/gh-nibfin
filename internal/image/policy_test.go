package image

import (
	"testing"

	"github.com/taco3064/gh-nibfin/internal/runnerhook"
)

// TestRunnerHookPolicy keeps the collaborator runner hook's image policy
// equal to the one runner add, init and upgrade apply here.
func TestRunnerHookPolicy(t *testing.T) {
	for _, c := range [][2]string{
		{runnerhook.ImageRepository, Repository},
		{runnerhook.SourceRepo, SourceRepo},
		{runnerhook.SignerWorkflow, signerWorkflow},
		{runnerhook.SourceRef, sourceRef},
	} {
		if c[0] != c[1] {
			t.Errorf("runnerhook has %q, image %q", c[0], c[1])
		}
	}
}
