package cli

import "context"

// Core's pending retirement response exits 2. Preserve its bounded stdout only
// for the fixed rebuild operation adapter; stderr never crosses this boundary.
type coreResultProcess struct {
	ctx     context.Context
	resolve func(string) (string, error)
}

func (p coreResultProcess) Run(name string, args []string, input []byte) ([]byte, error) {
	if p.resolve != nil {
		path, err := p.resolve(name)
		if err != nil {
			return nil, err
		}
		return runResolvedNativeProcess(p.ctx, name, path, args, input, true)
	}
	return runNativeProcess(p.ctx, name, args, input, true)
}

func rebuildResultProcess(process Process) Process {
	switch p := process.(type) {
	case NativeProcess:
		return coreResultProcess{ctx: context.Background()}
	case onboardingProcess:
		return coreResultProcess{ctx: p.ctx}
	default:
		return process
	}
}
