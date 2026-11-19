package cli

import (
	"io"
	"os"
	"testing"
)

func TestNativeInitPublicArgumentAndRootRefusal(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--unknown"}, {"--repo", "example/project", "unexpected"},
	} {
		err := (App{}).runInit(args, io.Discard)
		if err == nil || err.Error() != "INVALID_INIT_ARGUMENTS" {
			t.Fatalf("public argument refusal: %v", err)
		}
	}
	if os.Getuid() == 0 {
		err := (App{}).runManagedInit(InitPlan{}, true, io.Discard)
		if err == nil || err.Error() != "MANAGED_INSTALL_REQUIRES_INSTALLING_USER" {
			t.Fatalf("root-sensitive boundary must remain closed: %v", err)
		}
	}
}
