package cli

import (
	"context"
	"time"
)

// Core's pending retirement response exits 2. Preserve its bounded stdout only
// for the fixed rebuild operation adapter; stderr never crosses this boundary.
type coreResultProcess struct{ ctx context.Context }

func (p coreResultProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(p.ctx, 50*time.Minute)
	defer cancel()
	return runNativeProcess(ctx, name, args, input, true)
}

func rebuildResultProcess(process Process) Process {
	switch p := process.(type) {
	case NativeProcess:
		return coreResultProcess{context.Background()}
	case onboardingProcess:
		return coreResultProcess{p.ctx}
	default:
		return process
	}
}
