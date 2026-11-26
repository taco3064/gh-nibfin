package templates

import "github.com/taco3064/gh-nibfin/internal/runnerreq"

// The runner request contract, which nibfin-runner-request.yml answers and gh
// nibfin runner add uses. The values live in internal/runnerreq, which both
// sides import; see that package for the request and answer formats.
const (
	RunnerRequestFile          = runnerreq.RunnerRequestFile
	RunnerRequestTitle         = runnerreq.RunnerRequestTitle
	RunnerRequestMarker        = runnerreq.RunnerRequestMarker
	RunnerKeyIDField           = runnerreq.RunnerKeyIDField
	RunnerDeployKeyField       = runnerreq.RunnerDeployKeyField
	RunnerDeployKeyTitlePrefix = runnerreq.RunnerDeployKeyTitlePrefix
	RunnerRegistrationMarker   = runnerreq.RunnerRegistrationMarker
	RunnerRefusedMarker        = runnerreq.RunnerRefusedMarker
	RunnerRequesterField       = runnerreq.RunnerRequesterField
	RunnerExpiresField         = runnerreq.RunnerExpiresField
	RunnerReasonField          = runnerreq.RunnerReasonField
	RunnerTokenBegin           = runnerreq.RunnerTokenBegin
	RunnerTokenEnd             = runnerreq.RunnerTokenEnd
)
