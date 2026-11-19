package cli

// Native launchers persist the actual verifier group before waiting. Existing
// injectable Process implementations remain usable for controlled verification;
// they do not establish native host process-termination evidence.
type sourceVerifierProcess interface {
	runSourceVerifier(string, []string, func(int) error) ([]byte, error)
}

type sourceVerifier struct {
	process Process
	source  *coreSource
}

func (p sourceVerifier) Run(name string, args []string, input []byte) ([]byte, error) {
	if err := p.source.verifying(); err != nil {
		return nil, err
	}
	var data []byte
	var err error
	if native, ok := p.process.(sourceVerifierProcess); ok {
		data, err = native.runSourceVerifier(name, args, p.source.started)
	} else {
		data, err = p.process.Run(name, args, input)
	}
	if finishErr := p.source.finished(); finishErr != nil && err == nil {
		err = finishErr
	}
	return data, err
}
